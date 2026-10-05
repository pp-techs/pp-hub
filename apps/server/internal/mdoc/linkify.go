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

// linkify splits unlinked, non-code text runs around the autolinks marked
// would find in them, and settles the emoji shortcode tokens inside each run:
// marked tries the emoji tokenizer at every `:` the text reaches, but an
// autolink that started earlier runs straight through one
// (`www.a.example:smile:` is a link to `www.a.example:smile`).
func linkify(nodes []Node) []Node {
	var out []Node
	for i := 0; i < len(nodes); {
		if !linkifiable(nodes[i]) {
			out = append(out, nodes[i])
			i++
			continue
		}
		j := i + 1
		for j < len(nodes) && linkifiable(nodes[j]) && marksEqual(nodes[j].Marks, nodes[i].Marks) {
			j++
		}
		out = append(out, linkifyRun(nodes[i:j])...)
		i = j
	}
	return out
}

func linkifiable(n Node) bool {
	return n.Type == shortcodeType ||
		n.Type == "text" && !hasMark(n.Marks, "link") && !hasMark(n.Marks, "code")
}

func hasMark(marks []Mark, typ string) bool {
	for _, m := range marks {
		if m.Type == typ {
			return true
		}
	}
	return false
}

// shortcodeSpan is a shortcode token's byte range in a run's text.
type shortcodeSpan struct {
	start, end int
	node       Node
}

// linkifyRun scans one run of text and shortcode tokens sharing a mark set,
// as marked's inline lexer walks the same source: a shortcode token is read
// where the scan reaches it, an autolink wherever one starts, and a token an
// autolink covers is just part of the link's text.
func linkifyRun(run []Node) []Node {
	var b strings.Builder
	var tokens []shortcodeSpan
	for _, n := range run {
		start := b.Len()
		b.WriteString(n.Text)
		if n.Type == shortcodeType {
			tokens = append(tokens, shortcodeSpan{start, b.Len(), n})
		}
	}
	s, marks := b.String(), run[0].Marks
	if len(tokens) == 0 && !strings.ContainsAny(s, "@:.") {
		return run
	}
	var out []Node
	plain := 0   // start of the text not yet emitted
	runEnd := -1 // end of the email-local run holding i
	next := 0    // the first token not yet passed
	emitPlain := func(to int) {
		if plain < to {
			out = append(out, Node{Type: "text", Text: s[plain:to], Marks: marks})
		}
	}
	for i := 0; i < len(s); {
		for next < len(tokens) && tokens[next].start < i {
			next++ // swallowed by an autolink
		}
		if next < len(tokens) && tokens[next].start == i {
			emitPlain(i)
			token := tokens[next].node
			token.Marks = marks
			out = append(out, resolveShortcode(token))
			i = tokens[next].end
			plain, runEnd = i, -1
			next++
			continue
		}
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
		emitPlain(i)
		linkMarks := append([]Mark{{Type: "link", Attrs: map[string]any{"href": href, "title": nil}}}, marks...)
		out = append(out, Node{Type: "text", Text: text, Marks: linkMarks})
		i += len(text)
		plain, runEnd = i, -1
	}
	emitPlain(len(s))
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
