package mdoc

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/reearth/ygo/crdt"
)

// Comment fixtures (testdata/comments, from apps/web/scripts/mdoc-fixtures.ts)
// hold documents with comment marks, slash-editor's markdown for them (which
// drops the marks) and y-tiptap's Y update. The markdown Render writes for
// each is pinned in testdata/go-updates/comment-<name>.md (see TestGoUpdates).

type commentFixture struct {
	Name       string `json:"name"`
	JSON       Node   `json:"json"`
	JSOutput   string `json:"js_output"`
	YUpdateB64 string `json:"yupdate_b64"`
}

func loadCommentFixtures(t *testing.T) []commentFixture {
	t.Helper()
	files, err := filepath.Glob("testdata/comments/*.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no comment fixtures; run bun apps/web/scripts/mdoc-fixtures.ts")
	}
	out := make([]commentFixture, 0, len(files))
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var fx commentFixture
		if err := json.Unmarshal(raw, &fx); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		out = append(out, fx)
	}
	return out
}

// lineStartMarker finds a comment marker opening a line, container prefixes
// (quote, list, task) aside: where CommonMark would open an HTML block.
var lineStartMarker = regexp.MustCompile(`(?m)^(?:[ \t]*(?:>|[-*+]|\d+[.)])[ \t]*(?:\[[ xX]\][ \t]+)?)*[ \t]*<!-- /?slash:comment`)

// yTextKeys lists, per Y text delta insert in document order, its sorted
// attribute keys.
func yTextKeys(frag *crdt.YXmlFragment) [][]string {
	var out [][]string
	var visit func(c any)
	visit = func(c any) {
		switch x := c.(type) {
		case *crdt.YXmlElement:
			for _, cc := range x.Children() {
				visit(cc)
			}
		case *crdt.YXmlText:
			for _, d := range x.ToDelta() {
				keys := []string{}
				for k, v := range d.Attributes {
					if v != nil {
						keys = append(keys, k)
					}
				}
				sort.Strings(keys)
				out = append(out, keys)
			}
		}
	}
	for _, c := range frag.Children() {
		visit(c)
	}
	return out
}

func TestCommentFixtures(t *testing.T) {
	for _, fx := range loadCommentFixtures(t) {
		t.Run(fx.Name, func(t *testing.T) {
			want := normalize(t, fx.JSON)
			md := string(renderFixture(withTrailingParagraph(fx.JSON), nil))

			golden, err := os.ReadFile(filepath.Join("testdata", "go-updates", "comment-"+fx.Name+".md"))
			if err != nil {
				t.Fatalf("%v; run go test ./internal/mdoc/ -run TestGoUpdates -update", err)
			}
			if md != string(golden) {
				t.Errorf("Render:\n got %q\nwant %q (go-updates/comment-%s.md)", md, golden, fx.Name)
			}
			if got := commentMarkerAny.ReplaceAllString(md, ""); got != fx.JSOutput {
				t.Errorf("markers are not pure insertions into slash-editor's markdown:\n got %q\nwant %q", got, fx.JSOutput)
			}
			if loc := lineStartMarker.FindStringIndex(md); loc != nil {
				t.Errorf("a comment marker starts a line at %d: %q", loc[0], md)
			}

			parsed, err := Parse([]byte(md))
			if err != nil {
				t.Fatal(err)
			}
			if parsed.Lossy {
				t.Error("Parse flags the document lossy")
			}
			checkIDs(t, parsed.Doc, map[string]bool{})
			if got := normalize(t, withoutTrailingParagraph(t, parsed.Doc)); got != want {
				t.Errorf("Parse(Render):\n got %s\nwant %s", got, want)
			}
			if got := string(renderFixture(parsed.Doc, nil)); got != md {
				t.Errorf("Render(Parse(Render)):\n got %q\nwant %q", got, md)
			}

			jsUpdate, err := base64.StdEncoding.DecodeString(fx.YUpdateB64)
			if err != nil {
				t.Fatal(err)
			}
			d := crdt.New()
			if err := crdt.ApplyUpdateV1(d, jsUpdate, nil); err != nil {
				t.Fatal(err)
			}
			jsFrag := d.GetXmlFragment(FragmentName)
			if got := normalize(t, Read(jsFrag)); got != want {
				t.Errorf("Read(y-tiptap update):\n got %s\nwant %s", got, want)
			}
			goFrag := fill(fx.JSON)
			gotKeys, wantKeys := yTextKeys(goFrag), yTextKeys(jsFrag)
			if strings.Join(flatten(gotKeys), "|") != strings.Join(flatten(wantKeys), "|") {
				t.Errorf("Y text attribute keys:\n got %v\nwant %v (y-tiptap)", gotKeys, wantKeys)
			}
			if got := normalize(t, Read(fill(parsed.Doc))); got != normalize(t, parsed.Doc) {
				t.Errorf("Read(Fill(Parse)):\n got %s\nwant %s", got, normalize(t, parsed.Doc))
			}
		})
	}
}

func flatten(keys [][]string) []string {
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = strings.Join(k, ",")
	}
	return out
}

func TestCommentMarkersParse(t *testing.T) {
	const (
		o1 = `<!-- slash:comment {"threadId":"t1"} -->`
		c1 = `<!-- /slash:comment {"threadId":"t1"} -->`
		o2 = `<!-- slash:comment {"threadId":"t2"} -->`
		c2 = `<!-- /slash:comment {"threadId":"t2"} -->`
	)
	para := func(content ...Node) Node {
		return Node{Type: "doc", Content: []Node{{Type: "paragraph", Content: content}}}
	}
	txt := func(s string, marks ...Mark) Node { return Node{Type: "text", Text: s, Marks: marks} }
	cm := func(id string) Mark { return Mark{Type: "comment", Attrs: map[string]any{"threadId": id}} }
	bold := Mark{Type: "bold"}
	for _, tc := range []struct {
		name, md string
		want     Node
	}{
		{"pair", "a " + o1 + "b" + c1 + " c", para(txt("a "), txt("b", cm("t1")), txt(" c"))},
		{"empty pair", "a " + o1 + c1 + "b", para(txt("a b"))},
		{"close without open", "a " + c1 + "b", para(txt("a b"))},
		{"open never closed", "a " + o1 + "b", para(txt("a b"))},
		{"second open ignored", "a " + o1 + "b" + o1 + "c" + c1 + "d", para(txt("a "), txt("bc", cm("t1")), txt("d"))},
		{"close of another thread", "a " + o1 + "b" + c2 + "c" + c1, para(txt("a "), txt("bc", cm("t1")))},
		{"crossed", "a " + o1 + "b" + o2 + "c" + c1 + "d" + c2, para(txt("a "), txt("b", cm("t1")), txt("c", cm("t1"), cm("t2")), txt("d", cm("t2")))},
		{"malformed payload", "a " + `<!-- slash:comment {"threadId":} -->` + "b" + c1, para(txt("a b"))},
		{"no thread id", "a " + `<!-- slash:comment {"id":"t1"} -->` + "b" + c1, para(txt("a b"))},
		{"thread id not a string", "a " + `<!-- slash:comment {"threadId":7} -->` + "b" + c1, para(txt("a b"))},
		{"negative back", "ab" + `<!-- slash:comment {"threadId":"t1","back":-1} -->` + c1, para(txt("ab"))},
		{"fractional back", "ab" + `<!-- slash:comment {"threadId":"t1","back":1.5} -->` + c1, para(txt("ab"))},
		{"back", "abc" + `<!-- slash:comment {"threadId":"t1","back":2} -->` + "d" + c1, para(txt("a"), txt("bcd", cm("t1")))},
		{"back past the start", "ab" + `<!-- slash:comment {"threadId":"t1","back":9} -->` + c1 + " c", para(txt("ab", cm("t1")), txt(" c"))},
		{"close back", "a " + o1 + "bcd" + `<!-- /slash:comment {"threadId":"t1","back":2} -->`, para(txt("a "), txt("b", cm("t1")), txt("cd"))},
		{"close before open", "abcd" + `<!-- slash:comment {"threadId":"t1","back":1} -->` + `<!-- /slash:comment {"threadId":"t1","back":3} -->`, para(txt("abcd"))},
		{"escaped dashes", "a " + `<!-- slash:comment {"threadId":"x\u002d\u002dy"} -->` + "b" + `<!-- /slash:comment {"threadId":"x\u002d\u002dy"} -->`, para(txt("a "), txt("b", cm("x--y")))},
		{"across emphasis", "**a " + o1 + "b** c" + c1, para(txt("a ", bold), txt("b", bold, cm("t1")), txt(" c", cm("t1")))},
		{"line start", o1 + "Hello" + c1 + " **world**", para(txt("Hello", cm("t1")), txt(" "), txt("world", bold))},
		{"line start after soft break", "one\n" + o1 + "two" + c1 + " x", para(txt("one\n"), txt("two", cm("t1")), txt(" x"))},
		{"inside code span", "`a" + o1 + "b`", para(txt("a"+o1+"b", Mark{Type: "code"}))},
		{"link text", "[a " + o1 + "b" + c1 + "](https://x.y)", para(
			txt("a ", Mark{Type: "link", Attrs: map[string]any{"href": "https://x.y", "title": nil}}),
			txt("b", Mark{Type: "link", Attrs: map[string]any{"href": "https://x.y", "title": nil}}, cm("t1")),
		)},
		{"astral", "🎉" + o1 + "🎉x" + c1, para(txt("🎉"), txt("🎉x", cm("t1")))},
		{"around code span", "a " + o1 + "b `c` d" + c1, para(txt("a "), txt("b ", cm("t1")), txt("c", Mark{Type: "code"}), txt(" d", cm("t1")))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := Parse([]byte(tc.md))
			if err != nil {
				t.Fatal(err)
			}
			if parsed.Lossy {
				t.Error("Parse flags the document lossy")
			}
			if got, want := normalize(t, withoutTrailingParagraph(t, parsed.Doc)), normalize(t, tc.want); got != want {
				t.Errorf("Parse(%q):\n got %s\nwant %s", tc.md, got, want)
			}
		})
	}
}

// TestCommentMarkerLine: a comment marker alone on a line is a stray marker
// line like any other (swallowed), but no loss: comments are not content.
func TestCommentMarkerLine(t *testing.T) {
	parsed, err := Parse([]byte("<!-- slash:comment {\"threadId\":\"t1\"} -->\n\ntext\n\n- <!-- /slash:comment {\"threadId\":\"t1\"} -->\n"))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Lossy {
		t.Error("a stray comment marker line flags the document lossy")
	}
	want := Node{Type: "doc", Content: []Node{
		{Type: "paragraph", Content: []Node{{Type: "text", Text: "text"}}},
		{Type: "bulletList", Content: []Node{{Type: "listItem", Content: []Node{{Type: "paragraph", Content: []Node{}}}}}},
	}}
	if got := normalize(t, withoutTrailingParagraph(t, parsed.Doc)); got != normalize(t, want) {
		t.Errorf("Parse:\n got %s\nwant %s", got, normalize(t, want))
	}
}
