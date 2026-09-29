package mdoc

import (
	"regexp"
	"strings"
)

// GFM extended autolinks as marked finds them (its gfm `url` and `text`
// inline rules), applied to parsed text runs. goldmark's Linkify only looks
// after a space or an emphasis delimiter, marked also inside quotes,
// brackets and words: its text rule stops before every `http(s)://`,
// `ftp://` and `www.`, and one character at a time through a run of
// email-local characters that leads to an `@`, so an address is tried at
// every position.

var (
	urlHeadRe = regexp.MustCompile(`^(?:(?i:https?|ftp)://|www\.)[a-zA-Z0-9-]`)
	emailRe   = regexp.MustCompile(`^[A-Za-z0-9._+-]+@[a-zA-Z0-9_-]+(?:\.[a-zA-Z0-9_-]*[a-zA-Z0-9])+`)
	emailFull = regexp.MustCompile(`^[A-Za-z0-9._+-]+@[a-zA-Z0-9_-]+(?:\.[a-zA-Z0-9_-]*[a-zA-Z0-9])+$`)
)

// linkify splits unlinked, non-code text nodes around the autolinks marked
// would find in them.
func linkify(nodes []Node) []Node {
	var out []Node
	for _, n := range nodes {
		if n.Type != "text" || hasMark(n.Marks, "link") || hasMark(n.Marks, "code") {
			out = append(out, n)
			continue
		}
		out = append(out, linkifyText(n)...)
	}
	return out
}

func hasMark(marks []Mark, typ string) bool {
	for _, m := range marks {
		if m.Type == typ {
			return true
		}
	}
	return false
}

func linkifyText(n Node) []Node {
	s := n.Text
	if !strings.ContainsAny(s, "@:.") {
		return []Node{n}
	}
	var out []Node
	plain := 0   // start of the text not yet emitted
	runEnd := -1 // end of the email-local run holding i
	for i := 0; i < len(s); {
		if i >= runEnd {
			runEnd = i
			for runEnd < len(s) && isEmailLocal(s[runEnd]) {
				runEnd++
			}
			if runEnd == i {
				runEnd++
			}
		}
		text, href, ok := autolinkAt(s, i, runEnd < len(s) && s[runEnd] == '@')
		if !ok {
			i++
			continue
		}
		if plain < i {
			out = append(out, Node{Type: "text", Text: s[plain:i], Marks: n.Marks})
		}
		marks := append([]Mark{{Type: "link", Attrs: map[string]any{"href": href, "title": nil}}}, n.Marks...)
		out = append(out, Node{Type: "text", Text: text, Marks: marks})
		i += len(text)
		plain = i
		runEnd = -1
	}
	if plain == 0 {
		return []Node{n}
	}
	if plain < len(s) {
		out = append(out, Node{Type: "text", Text: s[plain:], Marks: n.Marks})
	}
	return out
}

// autolinkAt reports the autolink marked's url rule matches at s[i:];
// emailAhead says the email-local run at i ends in an `@`.
func autolinkAt(s string, i int, emailAhead bool) (text, href string, ok bool) {
	rest := s[i:]
	if urlHeadRe.MatchString(rest) {
		end := len(rest)
		for k, r := range rest {
			if r == '<' || jsIsSpace(r) {
				end = k
				break
			}
		}
		text = rest[:end]
		for {
			shorter := backpedal(text)
			if shorter == text {
				break
			}
			text = shorter
		}
		if text == "" {
			return "", "", false
		}
		if strings.HasPrefix(text, "www.") {
			return text, "http://" + text, true
		}
		return text, text, true
	}
	if !emailAhead {
		return "", "", false
	}
	email := emailRe.FindString(rest)
	for email != "" && len(email) < len(rest) && (rest[len(email)] == '-' || rest[len(email)] == '_') {
		// (?![-_]): back off to the longest shorter match not followed by - or _.
		shorter := ""
		for e := len(email) - 1; e > 0; e-- {
			if emailFull.MatchString(rest[:e]) && rest[e] != '-' && rest[e] != '_' {
				shorter = rest[:e]
				break
			}
		}
		email = shorter
	}
	if email == "" {
		return "", "", false
	}
	return email, "mailto:" + email, true
}

// backpedal is one exec of marked's _backpedal,
// /(?:[^?!.,:;*_'"~()&]+|\([^)]*\)|&(?![a-zA-Z0-9]+;$)|[?!.,:;*_'"~)]+(?!$))+/,
// anchored at the URL's start (a URL starts with a letter).
func backpedal(s string) string {
	const stop = "?!.,:;*_'\"~()&"
	const trailing = "?!.,:;*_'\"~)"
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case strings.IndexByte(stop, c) < 0:
			for i < len(s) && strings.IndexByte(stop, s[i]) < 0 {
				i++
			}
		case c == '(':
			k := strings.IndexByte(s[i+1:], ')')
			if k < 0 {
				return s[:i]
			}
			i += k + 2
		case c == '&':
			if entityRefAtEnd.MatchString(s[i+1:]) {
				return s[:i]
			}
			i++
		default:
			j := i
			for j < len(s) && strings.IndexByte(trailing, s[j]) >= 0 {
				j++
			}
			if j == len(s) {
				j-- // the run may not reach the end
			}
			if j <= i {
				return s[:i]
			}
			i = j
		}
	}
	return s
}

func isEmailLocal(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '+' || c == '-'
}

var entityRefAtEnd = regexp.MustCompile(`^[a-zA-Z0-9]+;$`)
