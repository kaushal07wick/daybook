package summarize

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/kaushal07wick/daybook/internal/provider"
	"github.com/kaushal07wick/daybook/internal/scrub"
	"github.com/kaushal07wick/daybook/internal/store"
)

// SessionSummary is the one shape every summarisation call must return.
type SessionSummary struct {
	Headline string   `json:"headline"`
	Project  string   `json:"project"`
	Work     []string `json:"work"`
	Tech     []string `json:"tech"`
	Numbers  []string `json:"numbers"`
	Facts    []string `json:"facts"`
	Hosts    []string `json:"hosts"`
	Outcome  string   `json:"outcome"`
}

// Summarizer runs the session pipeline against one provider.
type Summarizer struct {
	Store    *store.Store
	Provider provider.Provider
	Redact   []string
	Log      *slog.Logger
}

func (z *Summarizer) log() *slog.Logger {
	if z.Log == nil {
		return slog.Default()
	}
	return z.Log
}

// Session summarises one closed session end-to-end. A model that returns
// bad JSON twice is recorded as a failed summary, not an error: the session
// stays visible and can be retried with another provider.
func (z *Summarizer) Session(ctx context.Context, sess store.Session) error {
	start := time.Now()
	evs, err := z.Store.Events(sess.ID)
	if err != nil {
		return err
	}
	text := Render(evs)
	// The only place cloud gating happens: everything downstream sees
	// scrubbed text, including the reduce step.
	if z.Provider.Cloud() {
		text = scrub.Text(text, z.Redact)
	}
	budget := z.Provider.MaxInputTokens() * 8 / 10
	chunks := Chunk(text, budget)

	var sum SessionSummary
	var raw string
	if len(chunks) == 1 {
		sum, raw, err = z.ask(ctx, SessionSystem, header(sess)+chunks[0])
	} else {
		// Lists are unioned in code (a small model drops and duplicates
		// them); the model only merges headline, work and outcome.
		var lists SessionSummary
		var lastErr error
		partials := make([]string, 0, len(chunks))
		for i, c := range chunks {
			p, r, err := z.ask(ctx, SessionSystem, fmt.Sprintf("%s(part %d of %d)\n\n%s", header(sess), i+1, len(chunks), c))
			if err != nil {
				if ctx.Err() != nil || errors.Is(err, provider.ErrRateLimited) {
					return err
				}
				// One chunk the model chokes on (Ollama aborts repetition
				// loops with a 500) must not sink the other 30.
				z.log().Warn("chunk failed", "session", sess.ID, "part", i+1, "err", err)
				lastErr = err
				continue
			}
			if r != "" {
				continue // drop unparseable part, keep going
			}
			z.log().Debug("partial", "session", sess.ID, "part", i+1, "headline", p.Headline)
			lists.Tech = append(lists.Tech, p.Tech...)
			lists.Numbers = append(lists.Numbers, p.Numbers...)
			lists.Facts = append(lists.Facts, p.Facts...)
			lists.Hosts = append(lists.Hosts, p.Hosts...)
			lists.Outcome = p.Outcome
			b, _ := json.Marshal(SessionSummary{Headline: p.Headline, Project: p.Project, Work: p.Work, Outcome: p.Outcome})
			partials = append(partials, string(b))
		}
		switch {
		case len(partials) == 0 && lastErr != nil:
			return lastErr // provider down, not a model failure: retry next run
		case len(partials) == 0:
			raw = "all chunks failed"
		default:
			sum, raw, err = z.reduce(ctx, partials, budget)
			sum.Tech, sum.Numbers, sum.Facts, sum.Hosts = lists.Tech, lists.Numbers, lists.Facts, lists.Hosts
			if sum.Outcome == "" {
				sum.Outcome = lists.Outcome // last part's, when the merge garbled it
			}
		}
	}
	if err != nil {
		return err
	}
	sum.Work, sum.Tech, sum.Numbers, sum.Facts, sum.Hosts = dedupe(sum.Work), dedupe(notFiles(sum.Tech)), dedupe(sum.Numbers), dedupe(sum.Facts), dedupe(sum.Hosts)
	rec := store.Summary{SessionID: sess.ID, Provider: z.Provider.Name(), Model: z.Provider.Model(), CreatedAt: time.Now()}
	if raw != "" {
		rec.Raw = raw
		z.log().Warn("summary failed", "session", sess.ID, "project", sess.Project, "ms", time.Since(start).Milliseconds())
		return z.Store.PutSummary(rec)
	}
	if sum.Project == "" {
		sum.Project = sess.Project
	}
	b, _ := json.Marshal(sum)
	rec.JSON = string(b)
	if err := z.Store.PutSummary(rec); err != nil {
		return err
	}
	z.log().Info("summarised", "session", sess.ID, "project", sess.Project, "outcome", sum.Outcome, "ms", time.Since(start).Milliseconds())
	return z.Store.UpsertTerms(sess.ID, sess.EndedAt, map[string][]string{
		"tech": sum.Tech, "number": sum.Numbers, "host": sum.Hosts, "project": {sum.Project},
	})
}

// reduce folds partial records into one, in rounds sized to half the token
// budget: small enough that a 3B model does not loop on a wall of similar
// bullets, and a long session's partials never overflow the context.
func (z *Summarizer) reduce(ctx context.Context, partials []string, budget int) (SessionSummary, string, error) {
	budget /= 2
	for {
		groups := Chunk(strings.Join(partials, "\n\n"), budget)
		if len(groups) == 1 || len(groups) == len(partials) {
			// Fits in one call, or cannot shrink further: final merge.
			return z.ask(ctx, ReduceSystem, strings.Join(partials, "\n\n"))
		}
		next := make([]string, 0, len(groups))
		for _, g := range groups {
			if !strings.Contains(g, "\n\n") {
				next = append(next, g) // lone partial passes through
				continue
			}
			p, raw, err := z.ask(ctx, ReduceSystem, g)
			if err != nil {
				return p, "", err
			}
			if raw != "" {
				continue // drop a group the model could not merge
			}
			b, _ := json.Marshal(p)
			next = append(next, string(b))
		}
		if len(next) == 0 {
			return SessionSummary{}, "reduce failed", nil
		}
		partials = next
	}
}

// dedupe drops empty and repeated values (case-insensitively), keeping
// first occurrence and order. Never returns nil so JSON shows [] not null.
func dedupe(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, v := range in {
		v = strings.TrimSpace(v)
		k := strings.ToLower(v)
		if v == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, v)
	}
	return out
}

// notFiles drops file paths and file names a small model lists as "tech"
// (start.py, models/x.onnx, foo.service). Structural only: no opinions on
// what counts as technology. No js/ts: "Node.js" and "Vue.js" are tech.
func notFiles(in []string) []string {
	out := in[:0]
	for _, v := range in {
		if strings.ContainsAny(v, "/\\") || fileExt.MatchString(v) {
			continue
		}
		out = append(out, v)
	}
	return out
}

var fileExt = regexp.MustCompile(`\.(py|sh|go|md|txt|ya?ml|json|toml|env|service|pt|pth|onnx|engine|whl|log|conf|cfg|ini)$`)

func header(s store.Session) string {
	return fmt.Sprintf("Project: %s\nStarted: %s\nAgent: %s\n\n", s.Project, s.StartedAt.Format("2006-01-02 15:04"), s.Kind)
}

// ask calls the model once, retrying once on invalid JSON. On the second
// failure it returns the raw text instead of an error.
func (z *Summarizer) ask(ctx context.Context, system, user string) (SessionSummary, string, error) {
	var sum SessionSummary
	prompt := user
	for attempt := 0; attempt < 2; attempt++ {
		// MaxTokens bounds a small model's repetition loops (seen: 12k
		// tokens and a 5-minute hang); a truncated reply fails parsing and
		// is retried instead.
		res, err := z.Provider.Complete(ctx, provider.Request{System: system, User: prompt, JSON: true, MaxTokens: maxReplyTokens})
		if err != nil {
			return sum, "", err
		}
		txt := stripFence(res.Text)
		if json.Unmarshal([]byte(txt), &sum) == nil && sum.Headline != "" {
			sum.Outcome = strings.ToLower(strings.TrimSpace(sum.Outcome))
			if !validOutcome[sum.Outcome] {
				sum.Outcome = "" // small models garble enums; blank beats wrong
			}
			return sum, "", nil
		}
		if attempt == 1 {
			return sum, res.Text, nil
		}
		prompt = user + "\n\nYour previous reply was not valid JSON matching the schema. Reply with only the JSON object."
	}
	return sum, "", errors.New("unreachable")
}

const maxReplyTokens = 1024

var validOutcome = map[string]bool{"shipped": true, "partial": true, "blocked": true, "investigation": true}

// stripFence removes ```json fences small models love to add.
func stripFence(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}

// Drain summarises up to limit pending sessions with the given concurrency.
// Rate limits back off 30 s; other errors are logged and counted as failed.
func (z *Summarizer) Drain(ctx context.Context, limit, concurrency int) (done, failed int, err error) {
	pending, err := z.Store.Unsummarized(limit, z.Provider.Name(), z.Provider.Model())
	if err != nil {
		return 0, 0, err
	}
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(max(concurrency, 1))
	var mu sync.Mutex
	for _, s := range pending {
		g.Go(func() error {
			for {
				err := z.Session(ctx, s)
				if errors.Is(err, provider.ErrRateLimited) {
					select {
					case <-time.After(30 * time.Second):
						continue
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				mu.Lock()
				if err != nil {
					failed++
					z.log().Warn("summarize", "session", s.ID, "err", err)
				} else {
					done++
				}
				mu.Unlock()
				return nil
			}
		})
	}
	err = g.Wait()
	return done, failed, err
}
