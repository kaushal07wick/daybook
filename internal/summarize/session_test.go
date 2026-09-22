package summarize

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kaushal07wick/daybook/internal/provider"
	"github.com/kaushal07wick/daybook/internal/store"
)

type fakeProv struct {
	cloud bool
	calls []provider.Request
	reply func(n int) (string, error)
}

func (f *fakeProv) Name() string        { return "fake" }
func (f *fakeProv) Model() string       { return "m" }
func (f *fakeProv) Cloud() bool         { return f.cloud }
func (f *fakeProv) MaxInputTokens() int { return 100 }
func (f *fakeProv) Complete(_ context.Context, r provider.Request) (provider.Response, error) {
	f.calls = append(f.calls, r)
	t, err := f.reply(len(f.calls))
	return provider.Response{Text: t}, err
}

const good = `{"headline":"Fixed nvenc","project":"work/x","work":["Fixed nvenc"],"tech":["ffmpeg"],"numbers":["48 streams"],"facts":[],"hosts":["10.0.0.5"],"outcome":"shipped"}`

func seed(t *testing.T, text string) (*store.Store, store.Session) {
	st, _ := store.Open(":memory:")
	t.Cleanup(func() { _ = st.Close() })
	t0 := time.Unix(1_700_000_000, 0)
	_ = st.AppendEvents("claude", "/p", 1, []store.Event{{SessionID: "s", TS: t0, Role: "user", Text: text}, {SessionID: "s", TS: t0, Role: "tool", ToolName: "Bash", Text: "ok"}}, nil)
	_, _ = st.CloseIdle(t0.Add(time.Hour))
	sess, _ := st.Session("s")
	return st, sess
}

func TestSessionHappyPathStoresSummaryAndTerms(t *testing.T) {
	st, sess := seed(t, "fix nvenc on 10.0.0.5")
	fp := &fakeProv{reply: func(int) (string, error) { return good, nil }}
	z := &Summarizer{Store: st, Provider: fp}
	if err := z.Session(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fp.calls[0].User, "U: fix nvenc on 10.0.0.5") || !strings.Contains(fp.calls[0].User, "T(Bash): ok") {
		t.Fatalf("prompt %q", fp.calls[0].User)
	}
	sum, ok, _ := st.Summary("s")
	var got SessionSummary
	if !ok || json.Unmarshal([]byte(sum.JSON), &got) != nil || got.Headline != "Fixed nvenc" {
		t.Fatalf("%+v", sum)
	}
	terms, _ := st.Terms("tech", 10)
	if len(terms) != 1 || terms[0].Term != "ffmpeg" {
		t.Fatalf("%+v", terms)
	}
}

func TestCloudProviderGetsScrubbedText(t *testing.T) {
	st, sess := seed(t, "fix nvenc on 10.0.0.5")
	fp := &fakeProv{cloud: true, reply: func(int) (string, error) { return good, nil }}
	z := &Summarizer{Store: st, Provider: fp}
	_ = z.Session(context.Background(), sess)
	if strings.Contains(fp.calls[0].User, "10.0.0.5") || !strings.Contains(fp.calls[0].User, "<ip-1>") {
		t.Fatalf("leak: %q", fp.calls[0].User)
	}
}

func TestInvalidJSONRetriesThenStoresRaw(t *testing.T) {
	st, sess := seed(t, "x")
	fp := &fakeProv{reply: func(int) (string, error) { return "not json", nil }}
	z := &Summarizer{Store: st, Provider: fp}
	if err := z.Session(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	if len(fp.calls) != 2 || !strings.Contains(fp.calls[1].User, "not valid JSON") {
		t.Fatalf("calls %d", len(fp.calls))
	}
	sum, _, _ := st.Summary("s")
	if sum.JSON != "" || sum.Raw != "not json" {
		t.Fatalf("%+v", sum)
	}
}

func TestLongSessionIsChunkedAndReduced(t *testing.T) {
	st, sess := seed(t, strings.Repeat("word ", 300)) // ~375 tokens > 100*0.8
	fp := &fakeProv{reply: func(int) (string, error) { return good, nil }}
	z := &Summarizer{Store: st, Provider: fp}
	_ = z.Session(context.Background(), sess)
	if len(fp.calls) < 3 || fp.calls[len(fp.calls)-1].System != ReduceSystem {
		t.Fatalf("calls=%d", len(fp.calls))
	}
}

func TestChunkRespectsBoundaries(t *testing.T) {
	parts := Chunk("aaaa\n\nbbbb\n\ncccc", 3)
	if len(parts) != 3 || parts[1] != "bbbb" {
		t.Fatalf("%q", parts)
	}
}

func TestDrainCountsDoneAndFailed(t *testing.T) {
	st, _ := seed(t, "x")
	fp := &fakeProv{reply: func(int) (string, error) { return good, nil }}
	z := &Summarizer{Store: st, Provider: fp}
	done, failed, err := z.Drain(context.Background(), 10, 1)
	if err != nil || done != 1 || failed != 0 {
		t.Fatalf("done=%d failed=%d err=%v", done, failed, err)
	}
	// second drain: nothing pending
	done, _, _ = z.Drain(context.Background(), 10, 1)
	if done != 0 {
		t.Fatalf("done=%d", done)
	}
}

func TestReduceIsHierarchical(t *testing.T) {
	// 8 partials of ~32 tokens each against a 200-token budget (reduce uses
	// half): it must fold in rounds, never handing the model all at once.
	st, _ := seed(t, "x")
	fp := &fakeProv{reply: func(int) (string, error) { return good, nil }}
	z := &Summarizer{Store: st, Provider: fp}
	big := `{"headline":"h","work":["` + strings.Repeat("w", 60) + `"]}` // ~32 tokens
	partials := make([]string, 8)
	for i := range partials {
		partials[i] = big
	}
	sum, raw, err := z.reduce(context.Background(), partials, 2*z.Provider.MaxInputTokens())
	if err != nil || raw != "" || sum.Headline != "Fixed nvenc" {
		t.Fatalf("%+v %q %v", sum, raw, err)
	}
	// All 8 at once would be ~260 tokens; every call must stay well under
	// the provider's 200-token window (the fake reduce replies are ~67
	// tokens each, so the final merge of 3 is ~200).
	for _, c := range fp.calls {
		if provider.EstimateTokens(c.User) > 200 {
			t.Fatalf("reduce call over budget: %d tokens", provider.EstimateTokens(c.User))
		}
	}
}

func TestGarbledOutcomeIsBlanked(t *testing.T) {
	st, sess := seed(t, "x")
	fp := &fakeProv{reply: func(n int) (string, error) {
		if n == 1 {
			return `{"headline":"h","outcome":"shir"}`, nil
		}
		return `{"headline":"h","outcome":" Shipped"}`, nil
	}}
	z := &Summarizer{Store: st, Provider: fp}
	if err := z.Session(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	sum, _, _ := st.Summary("s")
	var got SessionSummary
	if json.Unmarshal([]byte(sum.JSON), &got) != nil || got.Outcome != "" {
		t.Fatalf("%+v", got)
	}
	_ = st.PutSummary(store.Summary{SessionID: "s", Provider: "x", Model: "y", Raw: "r", CreatedAt: sess.EndedAt})
	if err := z.Session(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	sum, _, _ = st.Summary("s")
	if json.Unmarshal([]byte(sum.JSON), &got) != nil || got.Outcome != "shipped" {
		t.Fatalf("%+v", got)
	}
}

func TestChunkListsAreUnionedInCode(t *testing.T) {
	st, sess := seed(t, strings.Repeat("word ", 300)) // 4+ chunks at a 100-token budget
	part := func(n int) string {
		return `{"headline":"h","work":["w` + string(rune('0'+n)) + `"],"tech":["Go","go"],"numbers":["` + string(rune('0'+n)) + ` ms"],"hosts":["h1"],"outcome":"partial"}`
	}
	fp := &fakeProv{reply: func(n int) (string, error) { return part(n), nil }}
	z := &Summarizer{Store: st, Provider: fp}
	if err := z.Session(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	sum, _, _ := st.Summary("s")
	var got SessionSummary
	if err := json.Unmarshal([]byte(sum.JSON), &got); err != nil {
		t.Fatal(err)
	}
	// Reduce replies carry only what the final ask returned; lists must
	// come from the chunk partials: one Go, one host, one number per chunk.
	if len(got.Tech) != 1 || len(got.Hosts) != 1 || len(got.Numbers) < 2 || got.Facts == nil {
		t.Fatalf("%+v", got)
	}
	for _, c := range fp.calls {
		if c.System == ReduceSystem && strings.Contains(c.User, `"tech":["Go"`) {
			t.Fatalf("reduce input carries lists: %s", c.User)
		}
	}
}

func TestFailedChunkIsSkippedButTotalFailureErrors(t *testing.T) {
	st, sess := seed(t, strings.Repeat("word ", 300))
	boom := errors.New("http 500: prediction aborted")
	fp := &fakeProv{reply: func(n int) (string, error) {
		if n == 1 {
			return "", boom
		}
		return good, nil
	}}
	z := &Summarizer{Store: st, Provider: fp}
	if err := z.Session(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	if sum, ok, _ := st.Summary("s"); !ok || sum.JSON == "" {
		t.Fatalf("%+v", sum)
	}
	fp = &fakeProv{reply: func(int) (string, error) { return "", boom }}
	z = &Summarizer{Store: st, Provider: fp}
	if err := z.Session(context.Background(), store.Session{ID: "s", Project: "p"}); !errors.Is(err, boom) {
		t.Fatalf("want provider error, got %v", err)
	}
}

func TestTechDropsFilePaths(t *testing.T) {
	got := notFiles([]string{"TensorRT", "start.py", "models/x.onnx", "mediamtx.service", "Node.js", "ffmpeg"})
	if strings.Join(got, ",") != "TensorRT,Node.js,ffmpeg" {
		t.Fatalf("%q", got)
	}
}
