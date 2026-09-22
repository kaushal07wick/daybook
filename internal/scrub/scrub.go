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
	// ipv6Run is a maximal-munch scan for hex/colon/dot characters: no
	// boundary groups, so it never consumes a separator a neighbouring
	// match needs (that was the bug in the boundary-group version — two
	// addresses one comma apart would have the comma eaten as the first
	// match's boundary, hiding the second from the scan entirely). Each run
	// is validated with net.ParseIP in the callback below, which is what
	// lets "::" compression through while rejecting colon-bearing
	// non-addresses like "12:30:45" or "16:9".
	ipv6Run = regexp.MustCompile(`[0-9A-Fa-f:.]+`)
)

func isHexByte(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}

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
	s = ipv6Run.ReplaceAllStringFunc(s, func(m string) string {
		if !strings.Contains(m, ":") {
			return m
		}
		// A run picks up a delimiter's colon when nothing separates it from
		// the address ("host:fe80::1"), and a sentence's trailing "."
		// when nothing separates it from the address ("2001:db8::1.").
		// Try the whole run first, then each way of peeling one of those
		// off, and keep whichever parses as a real address.
		hasDelimPrefix := len(m) >= 2 && m[0] == ':' && m[1] != ':' && isHexByte(m[1])
		hasPunctSuffix := len(m) >= 2 && (m[len(m)-1] == '.' || m[len(m)-1] == ':')
		var prefix, core, suffix string
		try := func(p, c, sf string) bool {
			if net.ParseIP(c) == nil {
				return false
			}
			prefix, core, suffix = p, c, sf
			return true
		}
		switch {
		case try("", m, ""):
		case hasPunctSuffix && try("", m[:len(m)-1], m[len(m)-1:]):
		case hasDelimPrefix && try(m[:1], m[1:], ""):
		case hasDelimPrefix && hasPunctSuffix && try(m[:1], m[1:len(m)-1], m[len(m)-1:]):
		default:
			return m
		}
		return prefix + assign(core) + suffix
	})
	return s
}
