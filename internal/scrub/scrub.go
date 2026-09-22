// Package scrub redacts secrets and identifiers before text may leave the
// machine. It is the only thing standing between a transcript and a cloud
// provider, so every pattern here has a golden-file case.
package scrub

import (
	"fmt"
	"net"
	"regexp"
	"sort"
	"strings"
)

var (
	pemBlock = regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`)
	urlCred  = regexp.MustCompile(`(://[^/\s:@]+:)[^@\s]+(@)`)
	// kvSecret requires a non-"<" non-word character (or start of string)
	// immediately before the keyword, so it never re-matches the literal
	// word "secret" that urlCred's own "<secret>" placeholder leaves behind.
	kvSecret   = regexp.MustCompile(`(?i)(^|[^<\w])(password|passwd|pass|token|secret|api[_-]?key|authorization: *bearer)\s*[=:]?\s*['"]?([^\s'"]+)`)
	flagSecret = regexp.MustCompile(`(\s-p\s+)(?:'[^']*'|"[^"]*"|[^\s]+)`)
	keyShape   = regexp.MustCompile(`\b(?:sk-[A-Za-z0-9_-]{20,}|AKIA[A-Z0-9]{16}|gh[pousr]_[A-Za-z0-9]{30,}|xox[abp]-[A-Za-z0-9-]{10,}|[A-Fa-f0-9]{32,}|[A-Za-z0-9+/]{40,}={0,2})\b`)
	email      = regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b`)
	ipv4       = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	// ipv6Candidate finds runs of hex/colon/dot characters that contain at
	// least one colon; net.ParseIP then decides which candidates are real
	// addresses. That correctly handles "::" compression (which a
	// fixed-group-count regex can't), while still leaving colon-bearing
	// non-addresses like "12:30:45" or "16:9" untouched.
	ipv6Candidate = regexp.MustCompile(`(^|[^0-9A-Fa-f:.])([0-9A-Fa-f:.]*:[0-9A-Fa-f:.]*)($|[^0-9A-Fa-f:.])`)
)

// Text returns s with secrets replaced by stable placeholders. The same IP,
// email or redact term maps to the same placeholder within one call.
func Text(s string, redact []string) string {
	s = pemBlock.ReplaceAllString(s, "<private-key>")
	s = urlCred.ReplaceAllString(s, "${1}<secret>${2}")
	s = kvSecret.ReplaceAllStringFunc(s, func(m string) string {
		sub := kvSecret.FindStringSubmatch(m)
		if strings.HasPrefix(sub[3], "<") { // already a placeholder
			return m
		}
		return strings.TrimSuffix(m, sub[3]) + "<secret>"
	})
	s = flagSecret.ReplaceAllString(s, "${1}<secret>")
	s = keyShape.ReplaceAllString(s, "<key>")
	s = numbered(s, email, "email")
	s = redactIPs(s)
	// longest first so "Acme Corp Ltd" wins over "Acme Corp"
	terms := append([]string(nil), redact...)
	sort.Slice(terms, func(i, j int) bool { return len(terms[i]) > len(terms[j]) })
	for i, t := range terms {
		if t == "" {
			continue
		}
		re := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(t))
		s = re.ReplaceAllString(s, fmt.Sprintf("<redacted-%d>", i+1))
	}
	return s
}

// numbered replaces every match of re with <kind-N>, N stable per distinct match.
func numbered(s string, re *regexp.Regexp, kind string) string {
	seen := map[string]int{}
	next := 1
	return re.ReplaceAllStringFunc(s, func(m string) string {
		if _, ok := seen[m]; !ok {
			seen[m] = next
			next++
		}
		return fmt.Sprintf("<%s-%d>", kind, seen[m])
	})
}

// redactIPs replaces IPv4 and IPv6 addresses with <ip-N>. Both families share
// one seen map and counter so numbering is dense (<ip-1>, <ip-2>, ...)
// regardless of which family is found first, instead of pre-counting
// already-placed placeholders (which double-counts repeats and skips
// numbers).
func redactIPs(s string) string {
	seen := map[string]int{}
	next := 1
	assign := func(tok string) string {
		n, ok := seen[tok]
		if !ok {
			n = next
			seen[tok] = n
			next++
		}
		return fmt.Sprintf("<ip-%d>", n)
	}
	s = ipv4.ReplaceAllStringFunc(s, assign)
	s = ipv6Candidate.ReplaceAllStringFunc(s, func(m string) string {
		sub := ipv6Candidate.FindStringSubmatch(m)
		pre, tok, post := sub[1], sub[2], sub[3]
		if !strings.Contains(tok, ":") || net.ParseIP(tok) == nil {
			return m
		}
		return pre + assign(tok) + post
	})
	return s
}
