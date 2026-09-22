// Package scrub redacts secrets and identifiers before text may leave the
// machine. It is the only thing standing between a transcript and a cloud
// provider, so every pattern here has a golden-file case.
package scrub

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var (
	pemBlock   = regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`)
	urlCred    = regexp.MustCompile(`(://[^/\s:@]+:)[^@\s]+(@)`)
	kvSecret   = regexp.MustCompile(`(?i)\b(password|passwd|pass|token|secret|api[_-]?key|authorization: *bearer)\s*[=:]?\s*['"]?([^\s'"]+)`)
	flagSecret = regexp.MustCompile(`(\s-p\s+)(?:'[^']*'|"[^"]*"|[^\s]+)`)
	keyShape   = regexp.MustCompile(`\b(?:sk-[A-Za-z0-9_-]{20,}|AKIA[A-Z0-9]{16}|gh[pousr]_[A-Za-z0-9]{30,}|xox[abp]-[A-Za-z0-9-]{10,}|[A-Fa-f0-9]{32,}|[A-Za-z0-9+/]{40,}={0,2})\b`)
	email      = regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b`)
	ipv4       = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	ipv6       = regexp.MustCompile(`\b(?:[A-Fa-f0-9]{1,4}:){2,7}[A-Fa-f0-9]{1,4}\b`)
)

// Text returns s with secrets replaced by stable placeholders. The same IP,
// email or redact term maps to the same placeholder within one call.
func Text(s string, redact []string) string {
	s = pemBlock.ReplaceAllString(s, "<private-key>")
	s = urlCred.ReplaceAllString(s, "${1}<secret>${2}")
	s = kvSecret.ReplaceAllStringFunc(s, func(m string) string {
		sub := kvSecret.FindStringSubmatch(m)
		if strings.HasPrefix(sub[2], "<") { // already a placeholder
			return m
		}
		return strings.TrimSuffix(m, sub[2]) + "<secret>"
	})
	s = flagSecret.ReplaceAllString(s, "${1}<secret>")
	s = keyShape.ReplaceAllString(s, "<key>")
	s = numbered(s, email, "email")
	s = numbered(s, ipv4, "ip")
	s = numbered(s, ipv6, "ip")
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
// ipv4 and ipv6 share the "ip" counter via a shared seen map per call — keep
// it simple: pass the same kind and let the closure own the map.
func numbered(s string, re *regexp.Regexp, kind string) string {
	seen := map[string]int{}
	// count existing placeholders so a second pass (ipv6 after ipv4) continues numbering
	existing := regexp.MustCompile(`<`+kind+`-(\d+)>`).FindAllStringSubmatch(s, -1)
	next := len(existing) + 1
	return re.ReplaceAllStringFunc(s, func(m string) string {
		if _, ok := seen[m]; !ok {
			seen[m] = next
			next++
		}
		return fmt.Sprintf("<%s-%d>", kind, seen[m])
	})
}
