package mdoc

import (
	"reflect"
	"strings"
	"testing"
)

const pagesMD = `<!-- slash:subPage {"pageId":"s1"} -->
[One](/edit/s1)

See <!-- slash:pageLink {"pageId":"l1"} -->[L](/edit/l1)<!-- /slash:pageLink --> and <!-- slash:mention {"id":"doc:l2","label":"Old"} -->@Old<!-- /slash:mention -->, <!-- slash:pageLink {"pageId":"s1"} -->[One](/edit/s1)<!-- /slash:pageLink -->.

- item

  <!-- slash:subPage {"pageId":"s2"} -->
  [Two](/edit/s2)

| a |
| --- |
| <!-- slash:pageLink {"pageId":"l1"} -->[L](/edit/l1)<!-- /slash:pageLink --> |

<!-- slash:subPage {"pageId":"s1"} -->
[One](/edit/s1)
`

func pagesDoc(t *testing.T) Node {
	t.Helper()
	parsed, err := Parse([]byte(pagesMD))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Lossy {
		t.Fatal("pages document parses lossy")
	}
	return parsed.Doc
}

func TestPageRefs(t *testing.T) {
	subs, links := PageRefs(pagesDoc(t))
	if want := []string{"s1", "s2"}; !reflect.DeepEqual(subs, want) {
		t.Errorf("sub-pages = %v, want %v", subs, want)
	}
	if want := []string{"l1", "l2", "s1"}; !reflect.DeepEqual(links, want) {
		t.Errorf("links = %v, want %v", links, want)
	}
	if subs, links := PageRefs(Node{Type: "doc"}); subs != nil || links != nil {
		t.Errorf("PageRefs(empty) = %v, %v", subs, links)
	}
}

func TestMapPageIDs(t *testing.T) {
	doc := pagesDoc(t)
	before := normalize(t, doc)
	mapped := MapPageIDs(doc, func(id string) string {
		if id == "l2" {
			return ""
		}
		return "new-" + id
	})
	if normalize(t, doc) != before {
		t.Fatal("MapPageIDs changed its input")
	}
	subs, links := PageRefs(mapped)
	if want := []string{"new-s1", "new-s2"}; !reflect.DeepEqual(subs, want) {
		t.Errorf("sub-pages = %v, want %v", subs, want)
	}
	if want := []string{"new-l1", "l2", "new-s1"}; !reflect.DeepEqual(links, want) {
		t.Errorf("links = %v, want %v", links, want)
	}
	// Only pageIds change.
	back := MapPageIDs(mapped, func(id string) string { return strings.TrimPrefix(id, "new-") })
	if normalize(t, back) != before {
		t.Error("mapping back does not restore the document")
	}
}

func TestRenderWithPages(t *testing.T) {
	doc := Node{Type: "doc", Content: []Node{
		{Type: "subPage", Attrs: map[string]any{"pageId": "a"}},
		{Type: "paragraph", Content: []Node{
			{Type: "text", Text: "x "},
			{Type: "pageLink", Attrs: map[string]any{"pageId": "b"}},
			{Type: "pageLink", Attrs: map[string]any{"pageId": nil}},
			{Type: "pageLink", Attrs: map[string]any{"pageId": ""}},
		}},
		{Type: "subPage", Attrs: map[string]any{}},
		{Type: "paragraph", Content: []Node{{Type: "pageLink"}}},
	}}
	got := string(Render(doc, nil))
	want := "<!-- slash:subPage {\"pageId\":\"a\"} -->\n[Untitled](/edit/a)\n\n" +
		"x <!-- slash:pageLink {\"pageId\":\"b\"} -->[Untitled](/edit/b)<!-- /slash:pageLink -->\n\n"
	if got != want {
		t.Errorf("Render:\n got %q\nwant %q", got, want)
	}

	got = string(RenderWith(doc, []byte("---\nicon: x\n---\n"), RenderOptions{
		PageTitle: func(id string) string {
			return map[string]string{"a": "  A [1]  ", "b": " "}[id]
		},
		PageHref: func(id string) string { return "https://h.example/p/" + id + " (x)" },
	}))
	want = "---\nicon: x\n---\n" +
		"<!-- slash:subPage {\"pageId\":\"a\"} -->\n[A \\[1\\]](<https://h.example/p/a (x)>)\n\n" +
		"x <!-- slash:pageLink {\"pageId\":\"b\"} -->[Untitled](<https://h.example/p/b (x)>)<!-- /slash:pageLink -->\n\n"
	if got != want {
		t.Errorf("RenderWith:\n got %q\nwant %q", got, want)
	}
}

func TestPagesPlainText(t *testing.T) {
	if got, want := PlainText(pagesDoc(t)), "See  and , .\nitem\na"; got != want {
		t.Errorf("PlainText = %q, want %q", got, want)
	}
}

func TestParseDocMention(t *testing.T) {
	parsed, err := Parse([]byte(`a <!-- slash:mention {"id":"doc:x","label":"X"} -->@X<!-- /slash:mention --> <!-- slash:mention {"id":"user:u","label":"U"} -->@U<!-- /slash:mention --> <!-- slash:mention {"id":"doc:","label":"E"} -->@E<!-- /slash:mention -->`))
	if err != nil {
		t.Fatal(err)
	}
	content := parsed.Doc.Content[0].Content
	if content[1].Type != "pageLink" || content[1].Attrs["pageId"] != "x" || len(content[1].Attrs) != 1 {
		t.Errorf("doc mention = %+v, want pageLink x", content[1])
	}
	if content[3].Type != "mention" || content[3].Attrs["id"] != "user:u" {
		t.Errorf("user mention = %+v", content[3])
	}
	if content[5].Type != "mention" || content[5].Attrs["id"] != "doc:" {
		t.Errorf("empty doc mention = %+v, want it kept", content[5])
	}
}

func TestFrontmatterMeta(t *testing.T) {
	for _, tc := range []struct {
		name, fm, icon, cover string
	}{
		{"nil", "", "", ""},
		{"not frontmatter", "icon: x\n", "", ""},
		{"unterminated", "---\nicon: x\n", "", ""},
		{"empty", "---\n---\n", "", ""},
		{"plain", "---\ntitle: T\nicon: 🎉\ncover: attachment:abc\n---\n", "🎉", "attachment:abc"},
		{"crlf", "---\r\nicon: 🎉\r\ncover: attachment:abc\r\n---\r\n", "🎉", "attachment:abc"},
		{"dots close, no newline", "---\nicon: a\n...", "a", ""},
		{"double quoted", "---\nicon: \"#\\uFE0F\\u20E3\"\ncover: \"attachment:a b\" # c\n---\n", "#️⃣", "attachment:a b"},
		{"surrogate pair", "---\nicon: \"\\uD83C\\uDF89\"\n---\n", "🎉", ""},
		{"single quoted", "---\nicon: 'it''s'\n---\n", "it's", ""},
		{"comment", "---\nicon: 🎉 # party\n---\n", "🎉", ""},
		{"null", "---\nicon: ~\ncover: null\n---\n", "", ""},
		{"no value", "---\nicon:\ncover:\n  nested: x\n---\n", "", ""},
		{"block scalar", "---\nicon: |\n  x\n---\n", "", ""},
		{"indented is nested", "---\nmeta:\n  icon: x\n---\n", "", ""},
		{"first wins", "---\nicon: a\nicon: b\n---\n", "a", ""},
		{"no space after colon", "---\nicon:x\n---\n", "", ""},
		{"bad quote", "---\nicon: \"x\n---\n", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var fm []byte
			if tc.fm != "" {
				fm = []byte(tc.fm)
			}
			icon, cover := FrontmatterMeta(fm)
			if icon != tc.icon || cover != tc.cover {
				t.Errorf("FrontmatterMeta(%q) = %q, %q, want %q, %q", tc.fm, icon, cover, tc.icon, tc.cover)
			}
		})
	}
}

func TestSetFrontmatterMeta(t *testing.T) {
	for _, tc := range []struct {
		name, fm, icon, cover, want string
		wantNil                     bool
	}{
		{name: "nil, nothing to set", wantNil: true},
		{name: "create", icon: "🎉", cover: "attachment:abc", want: "---\nicon: 🎉\ncover: attachment:abc\n---\n"},
		{name: "create cover only", cover: "attachment:abc", want: "---\ncover: attachment:abc\n---\n"},
		{
			name: "keep others, replace in place",
			fm:   "---\ntitle: T\nicon: 🙂 # old\ntags: [a, b]\n---\n",
			icon: "🎉", cover: "attachment:x",
			want: "---\ntitle: T\nicon: 🎉\ntags: [a, b]\ncover: attachment:x\n---\n",
		},
		{
			name: "remove",
			fm:   "---\ntitle: T\nicon: 🎉\ncover: attachment:x\n---\n",
			want: "---\ntitle: T\n---\n",
		},
		{name: "remove last keys", fm: "---\nicon: 🎉\n\ncover: attachment:x\n---\n", wantNil: true},
		{
			name: "unchanged keeps bytes",
			fm:   "---\nicon: \"🎉\"   # keep me\ncover: 'attachment:x'\n---\n",
			icon: "🎉", cover: "attachment:x",
			want: "---\nicon: \"🎉\"   # keep me\ncover: 'attachment:x'\n---\n",
		},
		{name: "unchanged empty block", fm: "---\n---\n", want: "---\n---\n"},
		{
			name: "drops continuation and duplicates",
			fm:   "---\nicon: |\n  old\nicon: dup\nz: 1\n---\n",
			icon: "🎉",
			want: "---\nicon: 🎉\nz: 1\n---\n",
		},
		{
			name: "crlf",
			fm:   "---\r\ntitle: T\r\n---\r\n",
			icon: "🎉",
			want: "---\r\ntitle: T\r\nicon: 🎉\r\n---\r\n",
		},
		{
			name: "close without newline",
			fm:   "---\ntitle: T\n---",
			icon: "🎉",
			want: "---\ntitle: T\nicon: 🎉\n---",
		},
		{
			name: "quoted when needed",
			icon: "#️⃣", cover: "a: b",
			want: "---\nicon: \"#️⃣\"\ncover: \"a: b\"\n---\n",
		},
		{name: "quoted reserved word", icon: "yes", want: "---\nicon: \"yes\"\n---\n"},
		{name: "quoted number", icon: "1️⃣", want: "---\nicon: \"1️⃣\"\n---\n"},
		{name: "quoted control", icon: "a\nb", want: "---\nicon: \"a\\nb\"\n---\n"},
		{name: "not frontmatter", fm: "nope", icon: "x", want: "nope"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var fm []byte
			if tc.fm != "" {
				fm = []byte(tc.fm)
			}
			got := SetFrontmatterMeta(fm, tc.icon, tc.cover)
			if tc.wantNil {
				if got != nil {
					t.Errorf("SetFrontmatterMeta = %q, want nil", got)
				}
				return
			}
			if string(got) != tc.want {
				t.Errorf("SetFrontmatterMeta:\n got %q\nwant %q", got, tc.want)
			}
			if tc.fm == "nope" {
				return
			}
			if icon, cover := FrontmatterMeta(got); icon != tc.icon || cover != tc.cover {
				t.Errorf("FrontmatterMeta(result) = %q, %q, want %q, %q", icon, cover, tc.icon, tc.cover)
			}
			if strings.HasSuffix(string(got), "\n") {
				parsed, err := Parse(append(append([]byte(nil), got...), "# x\n"...))
				if err != nil {
					t.Fatal(err)
				}
				if string(parsed.Frontmatter) != string(got) {
					t.Errorf("Parse does not split the result off as frontmatter: %q", parsed.Frontmatter)
				}
			}
		})
	}
}
