package mdoc

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/reearth/ygo/crdt"
)

// The golden fixtures come from apps/web/scripts/mdoc-fixtures.ts (run
// `bun apps/web/scripts/mdoc-fixtures.ts` from the repo root), i.e. from
// slash-editor itself.
//
// Documents are compared after normalize: ids dropped, every schema
// attribute present with its default filled in (ProseMirror's
// Node.fromJSON, so an absent attribute, an explicit null and y-tiptap's
// omitted null all compare equal), numbers as float64.

var update = flag.Bool("update", false, "rewrite testdata/go-updates")

type fixture struct {
	Name        string `json:"name"`
	Input       string `json:"input"`
	Frontmatter string `json:"frontmatter"`
	JSON        Node   `json:"json"`
	Output      string `json:"output"`
	YUpdateB64  string `json:"yupdate_b64"`
	ExpectLossy bool   `json:"expect_lossy"`
}

func loadFixtures(t *testing.T) []fixture {
	t.Helper()
	files, err := filepath.Glob("testdata/fixtures/*.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no fixtures; run bun apps/web/scripts/mdoc-fixtures.ts")
	}
	out := make([]fixture, 0, len(files))
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var fx fixture
		if err := json.Unmarshal(raw, &fx); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		out = append(out, fx)
	}
	return out
}

// normalize returns n as canonical JSON (see the file comment).
func normalize(t *testing.T, n Node) string {
	t.Helper()
	raw, err := json.MarshalIndent(normalizeNode(n), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	raw, _ = json.MarshalIndent(v, "", "  ")
	return string(raw)
}

func normalizeNode(n Node) Node {
	out := Node{Type: n.Type, Text: n.Text}
	if defaults, ok := nodeAttrDefaults[n.Type]; ok {
		out.Attrs = withDefaults(n.Attrs, defaults)
	} else if n.Attrs != nil {
		out.Attrs = maps.Clone(n.Attrs)
	}
	delete(out.Attrs, "id")
	for _, m := range n.Marks {
		mm := Mark{Type: m.Type, Attrs: m.Attrs}
		if defaults, ok := markAttrDefaults[m.Type]; ok {
			mm.Attrs = withDefaults(m.Attrs, defaults)
		}
		out.Marks = append(out.Marks, mm)
	}
	// Node.fromJSON orders marks by schema rank; parseMarkdown's raw JSON
	// keeps parse order.
	sort.SliceStable(out.Marks, func(i, j int) bool { return markRank(out.Marks[i].Type) < markRank(out.Marks[j].Type) })
	for _, c := range n.Content {
		out.Content = append(out.Content, normalizeNode(c))
	}
	return out
}

// withoutTrailingParagraph drops the empty paragraph Parse ends with.
func withoutTrailingParagraph(t *testing.T, doc Node) Node {
	t.Helper()
	n := len(doc.Content)
	if n == 0 || !isEmptyParagraph(doc.Content[n-1]) {
		t.Fatalf("document does not end with an empty paragraph")
	}
	doc.Content = doc.Content[:n-1]
	return doc
}

func withTrailingParagraph(doc Node) Node {
	doc.Content = append(append([]Node(nil), doc.Content...), Node{Type: "paragraph"})
	return doc
}

func checkIDs(t *testing.T, n Node, seen map[string]bool) {
	t.Helper()
	id, has := n.Attrs["id"]
	if !hasIDAttr(n.Type) {
		if has {
			t.Errorf("%s carries an id", n.Type)
		}
	} else {
		s, ok := id.(string)
		switch {
		case !ok || len(s) != idLength || strings.Trim(s, idAlphabet) != "":
			t.Errorf("%s has id %v", n.Type, id)
		case seen[s]:
			t.Errorf("%s reuses id %s", n.Type, s)
		}
		seen[s] = true
	}
	for _, c := range n.Content {
		checkIDs(t, c, seen)
	}
}

func applyUpdate(t *testing.T, update []byte) Node {
	t.Helper()
	d := crdt.New()
	if err := crdt.ApplyUpdateV1(d, update, nil); err != nil {
		t.Fatal(err)
	}
	return Read(d.GetXmlFragment(FragmentName))
}

func fill(doc Node) *crdt.YXmlFragment {
	d := crdt.New()
	frag := d.GetXmlFragment(FragmentName)
	d.Transact(func(txn *crdt.Transaction) { Fill(txn, frag, doc) })
	return frag
}

func TestSchemaMatchesFixtures(t *testing.T) {
	raw, err := os.ReadFile("testdata/schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Nodes     map[string]map[string]any `json:"nodes"`
		Marks     map[string]map[string]any `json:"marks"`
		MarkOrder []string                  `json:"mark_order"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(schema.Nodes, nodeAttrDefaults) {
		t.Errorf("node attrs differ from the schema:\n got %v\nwant %v", nodeAttrDefaults, schema.Nodes)
	}
	if !reflect.DeepEqual(schema.Marks, markAttrDefaults) {
		t.Errorf("mark attrs differ from the schema:\n got %v\nwant %v", markAttrDefaults, schema.Marks)
	}
	if !reflect.DeepEqual(schema.MarkOrder, markOrder) {
		t.Errorf("mark order %v, schema has %v", markOrder, schema.MarkOrder)
	}
}

func TestFixtures(t *testing.T) {
	for _, fx := range loadFixtures(t) {
		t.Run(fx.Name, func(t *testing.T) {
			want := normalize(t, fx.JSON)
			wantMD := fx.Frontmatter + fx.Output

			parsed, err := Parse([]byte(fx.Input))
			if err != nil {
				t.Fatal(err)
			}
			if parsed.Lossy != fx.ExpectLossy {
				t.Errorf("Lossy = %v, want %v", parsed.Lossy, fx.ExpectLossy)
			}
			if got := string(parsed.Frontmatter); got != fx.Frontmatter || (fx.Frontmatter == "") != (parsed.Frontmatter == nil) {
				t.Errorf("Frontmatter = %q, want %q", got, fx.Frontmatter)
			}
			checkIDs(t, parsed.Doc, map[string]bool{})
			if got := normalize(t, withoutTrailingParagraph(t, parsed.Doc)); got != want {
				t.Errorf("Parse:\n got %s\nwant %s", got, want)
			}

			if got := string(Render(withTrailingParagraph(fx.JSON), []byte(fx.Frontmatter))); got != wantMD {
				t.Errorf("Render(json):\n got %q\nwant %q", got, wantMD)
			}
			if got := string(Render(parsed.Doc, parsed.Frontmatter)); got != wantMD {
				t.Errorf("Render(Parse):\n got %q\nwant %q", got, wantMD)
			}

			yupdate, err := base64.StdEncoding.DecodeString(fx.YUpdateB64)
			if err != nil {
				t.Fatal(err)
			}
			if got := normalize(t, applyUpdate(t, yupdate)); got != want {
				t.Errorf("Read(yupdate):\n got %s\nwant %s", got, want)
			}

			read := Read(fill(parsed.Doc))
			checkIDs(t, read, map[string]bool{})
			if got, want := normalize(t, read), normalize(t, parsed.Doc); got != want {
				t.Errorf("Read(Fill):\n got %s\nwant %s", got, want)
			}
			if got := string(Render(read, parsed.Frontmatter)); got != wantMD {
				t.Errorf("Render(Read(Fill(Parse))):\n got %q\nwant %q", got, wantMD)
			}

			seed, err := Seed(parsed.Doc)
			if err != nil {
				t.Fatal(err)
			}
			if got, want := normalize(t, applyUpdate(t, seed)), normalize(t, parsed.Doc); got != want {
				t.Errorf("Read(Seed):\n got %s\nwant %s", got, want)
			}
		})
	}
}

func TestParseFreshIDs(t *testing.T) {
	a, _ := Parse([]byte("# a\n\n- b\n"))
	b, _ := Parse([]byte("# a\n\n- b\n"))
	if a.Doc.Content[0].Attrs["id"] == b.Doc.Content[0].Attrs["id"] {
		t.Error("two parses share an id")
	}
}

// TestFillAppends pins Fill appending to a fragment that already holds content.
func TestFillAppends(t *testing.T) {
	first, _ := Parse([]byte("one\n"))
	second, _ := Parse([]byte("two\n"))
	d := crdt.New()
	frag := d.GetXmlFragment(FragmentName)
	d.Transact(func(txn *crdt.Transaction) { Fill(txn, frag, first.Doc) })
	d.Transact(func(txn *crdt.Transaction) { Fill(txn, frag, second.Doc) })
	both := Node{Type: "doc", Content: append(append([]Node(nil), first.Doc.Content...), second.Doc.Content...)}
	if got, want := normalize(t, Read(frag)), normalize(t, both); got != want {
		t.Errorf("after two Fills:\n got %s\nwant %s", got, want)
	}
}

// TestGoUpdates keeps testdata/go-updates/<name>.b64 = base64 of
// Seed(Parse(input)) for every fixture; apps/web/tests/mdoc-go-updates.test.ts
// decodes them with yjs + y-tiptap (base64 because Vite can import text, not
// bytes, and the web tests have no Node fs). -update rewrites a file only when its document
// changed (ids and Y client ids are random).
func TestGoUpdates(t *testing.T) {
	dir := filepath.Join("testdata", "go-updates")
	if *update {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	names := map[string]bool{}
	for _, fx := range loadFixtures(t) {
		names[fx.Name+".b64"] = true
		parsed, err := Parse([]byte(fx.Input))
		if err != nil {
			t.Fatal(err)
		}
		want := normalize(t, parsed.Doc)
		file := filepath.Join(dir, fx.Name+".b64")
		existing, err := os.ReadFile(file)
		if err == nil {
			existing, err = base64.StdEncoding.DecodeString(strings.TrimSpace(string(existing)))
		}
		current := err == nil && normalize(t, applyUpdate(t, existing)) == want
		if current {
			continue
		}
		if !*update {
			t.Errorf("%s is stale; run go test ./internal/mdoc/ -run TestGoUpdates -update", file)
			continue
		}
		seed, err := Seed(parsed.Doc)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(base64.StdEncoding.EncodeToString(seed)+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if names[e.Name()] {
			continue
		}
		if !*update {
			t.Errorf("%s has no fixture", e.Name())
			continue
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
			t.Fatal(err)
		}
	}
}
