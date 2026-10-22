package apppkg

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"errors"
	"io/fs"
	"strconv"
	"strings"
	"testing"
)

const minimalManifest = `{
  "id": "tasks", "name": "Tasks", "version": "1.0.0", "description": "A shared task list",
  "author": "Acme", "engines": {"sdk": 1},
  "nodeKinds": [{"kind": "tasks.list", "label": "Task list"}],
  "web": {"entry": "index.html"}
}`

type zentry struct {
	name    string
	body    []byte
	mode    fs.FileMode
	rawSize uint64 // when set, written with CreateRaw claiming this uncompressed size
}

func buildZip(t *testing.T, entries []zentry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		if e.mode != 0 {
			h.SetMode(e.mode)
		}
		if e.rawSize != 0 {
			var c bytes.Buffer
			fw, _ := flate.NewWriter(&c, flate.BestCompression)
			fw.Write(e.body)
			fw.Close()
			h.CompressedSize64 = uint64(c.Len())
			h.UncompressedSize64 = e.rawSize
			w, err := zw.CreateRaw(h)
			if err != nil {
				t.Fatal(err)
			}
			w.Write(c.Bytes())
			continue
		}
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		w.Write(e.body)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func f(name, body string) zentry { return zentry{name: name, body: []byte(body)} }

func withManifest(m string, extra ...zentry) []zentry {
	return append([]zentry{f("pp-app.json", m), f("index.html", "<html></html>")}, extra...)
}

func patch(old, new string) string {
	if !strings.Contains(minimalManifest, old) {
		panic("patch: " + old)
	}
	return strings.Replace(minimalManifest, old, new, 1)
}

func TestReadValid(t *testing.T) {
	a := buildZip(t, []zentry{f("pp-app.json", minimalManifest), f("index.html", "<html></html>"), f("app.js", "x"), f(".DS_Store", "j"), f("__MACOSX/._x", "j"), {name: "assets/"}})
	b := buildZip(t, []zentry{f("app.js", "x"), f("index.html", "<html></html>"), f("pp-app.json", minimalManifest)})
	ba, err := Read(a)
	if err != nil {
		t.Fatal(err)
	}
	bb, err := Read(b)
	if err != nil {
		t.Fatal(err)
	}
	if ba.Digest != bb.Digest || len(ba.Digest) != 64 {
		t.Fatalf("digest unstable: %s vs %s", ba.Digest, bb.Digest)
	}
	if len(ba.Files) != 3 || ba.Files[0].Path != "app.js" || ba.Files[1].Path != "index.html" {
		t.Fatalf("files: %+v", ba.Files)
	}
	if ba.Files[1].ContentType != "text/html; charset=utf-8" {
		t.Fatal(ba.Files[1].ContentType)
	}
	k := ba.Manifest.NodeKinds[0]
	if k.NewLabel != "New Task list" || k.DefaultName != "Task list" {
		t.Fatalf("defaults: %+v", k)
	}
	if ba.Manifest.Backend != nil || ba.Manifest.SDK != 1 {
		t.Fatalf("manifest: %+v", ba.Manifest)
	}
}

func TestReadStripsTopDir(t *testing.T) {
	z := buildZip(t, []zentry{{name: "tasks-main/"}, f("tasks-main/pp-app.json", minimalManifest), f("tasks-main/index.html", "<html></html>"), f("tasks-main/src/a.js", "x")})
	b, err := Read(z)
	if err != nil {
		t.Fatal(err)
	}
	if b.Files[0].Path != "index.html" || b.Files[2].Path != "src/a.js" {
		t.Fatalf("%+v", b.Files)
	}
}

func TestReadFull(t *testing.T) {
	m := `{"id":"tasks","name":"Tasks","version":"1.2.3-beta.1","description":"d","author":"a","license":"MIT",
"homepage":"https://acme.example","engines":{"sdk":1},"icon":"icon.svg",
"nodeKinds":[{"kind":"tasks.list","label":"L","newLabel":"Make","defaultName":"Untitled","newHint":"h","icon":"i/l.png",
"seed":[{"collection":"items","data":{"text":"First","done":false}}]}],
"web":{"entry":"web/index.html","connect":["https://api.example.com","https://x.example:8443"]},
"backend":{"url":"https://tasks.example.com/pp"},
"notifications":{"assigned":"{actor} assigned you: {excerpt}"},
"translations":{"vi":{"Tasks":"Công việc"},"pt-BR":{"Tasks":"Tarefas"}}}`
	z := buildZip(t, []zentry{f("pp-app.json", m), f("web/index.html", "x"), f("icon.svg", "<svg/>"), f("i/l.png", "p")})
	b, err := Read(z)
	if err != nil {
		t.Fatal(err)
	}
	k := b.Manifest.NodeKinds[0]
	if k.NewLabel != "Make" || k.DefaultName != "Untitled" || string(k.Seed[0].Data) != `{"text":"First","done":false}` {
		t.Fatalf("%+v", k)
	}
	if b.Manifest.Backend == nil || b.Manifest.Backend.URL != "https://tasks.example.com/pp" {
		t.Fatal("backend")
	}
}

func TestReadRejects(t *testing.T) {
	zeros := make([]byte, MaxFileBytes+1)
	nine := make([]byte, 9<<20)
	many := withManifest(minimalManifest)
	for i := range MaxFiles {
		many = append(many, f("f/"+strconv.Itoa(i), ""))
	}
	totalEntries := withManifest(minimalManifest)
	totalLying := withManifest(minimalManifest)
	for i := range 6 {
		totalEntries = append(totalEntries, zentry{name: "big" + strconv.Itoa(i), body: nine, rawSize: uint64(len(nine))})
		totalLying = append(totalLying, zentry{name: "big" + strconv.Itoa(i), body: nine, rawSize: 10})
	}
	seedObj := `"seed":[{"collection":"items","data":%s}]}]`
	cases := []struct {
		name    string
		entries []zentry
		raw     []byte
		want    string
	}{
		{"traversal", withManifest(minimalManifest, f("a/../b", "x")), nil, `".."`},
		{"absolute", withManifest(minimalManifest, f("/etc/passwd", "x")), nil, "absolute"},
		{"backslash", withManifest(minimalManifest, f(`a\b`, "x")), nil, "backslash"},
		{"symlink", withManifest(minimalManifest, zentry{name: "link", body: []byte("/etc"), mode: fs.ModeSymlink | 0o777}), nil, "symlink"},
		{"duplicate", withManifest(minimalManifest, f("index.html", "y")), nil, "duplicate"},
		{"too many files", many, nil, "more than 2000"},
		{"file too big lying header", withManifest(minimalManifest, zentry{name: "big", body: zeros, rawSize: 10}), nil, `"big"`},
		{"file too big", withManifest(minimalManifest, zentry{name: "big", body: zeros, rawSize: uint64(len(zeros))}), nil, `"big" is larger`},
		{"total too big lying header", totalLying, nil, `"big0"`},
		{"total too big", totalEntries, nil, "total uncompressed"},
		{"oversize upload", nil, make([]byte, MaxUploadBytes+1), "upload is larger"},
		{"no manifest", []zentry{f("index.html", "x")}, nil, "pp-app.json not found"},
		{"missing entry", []zentry{f("pp-app.json", minimalManifest)}, nil, `web.entry "index.html" is not in the package`},
		{"entry not html", withManifest(patch(`"entry": "index.html"`, `"entry": "index.js"`), f("index.js", "x")), nil, "must be an .html"},
		{"missing icon", withManifest(patch(`"author": "Acme",`, `"author": "Acme", "icon": "icon.svg",`)), nil, `icon "icon.svg" is not in the package`},
		{"bad icon ext", withManifest(patch(`"author": "Acme",`, `"author": "Acme", "icon": "icon.gif",`), f("icon.gif", "x")), nil, ".svg or .png"},
		{"icon too big", withManifest(patch(`"label": "Task list"`, `"label": "Task list", "icon": "i.png"`), zentry{name: "i.png", body: make([]byte, 300<<10)}), nil, "256 KiB"},
		{"unknown top-level", withManifest(patch(`"id": "tasks",`, `"id": "tasks", "extra": 1,`)), nil, `unknown field "extra"`},
		{"unknown nested", withManifest(patch(`"label": "Task list"`, `"label": "Task list", "color": "red"`)), nil, `unknown field "color"`},
		{"unknown engines", withManifest(patch(`{"sdk": 1}`, `{"sdk": 1, "node": 2}`)), nil, `unknown field "node"`},
		{"trailing data", withManifest(minimalManifest + "{}"), nil, "after the JSON"},
		{"bad id", withManifest(patch(`"id": "tasks"`, `"id": "Tasks"`)), nil, `id "Tasks"`},
		{"bad kind prefix", withManifest(patch(`"tasks.list"`, `"x.y"`)), nil, `nodeKinds[0].kind "x.y" must start with "tasks."`},
		{"bad kind suffix", withManifest(patch(`"tasks.list"`, `"tasks.L"`)), nil, `nodeKinds[0].kind "tasks.L"`},
		{"duplicate kind", withManifest(patch(`{"kind": "tasks.list", "label": "Task list"}`, `{"kind": "tasks.list", "label": "A"},{"kind": "tasks.list", "label": "B"}`)), nil, "declared twice"},
		{"no kinds", withManifest(patch(`[{"kind": "tasks.list", "label": "Task list"}]`, `[]`)), nil, "1 to 8"},
		{"bad version", withManifest(patch(`"1.0.0"`, `"1.0"`)), nil, `version "1.0"`},
		{"wrong sdk", withManifest(patch(`{"sdk": 1}`, `{"sdk": 2}`)), nil, "supports sdk 1"},
		{"name too long", withManifest(patch(`"name": "Tasks"`, `"name": "`+strings.Repeat("n", 61)+`"`)), nil, "name must be at most 60"},
		{"blank author", withManifest(patch(`"author": "Acme"`, `"author": " "`)), nil, "author is required"},
		{"bad homepage", withManifest(patch(`"author": "Acme",`, `"author": "Acme", "homepage": "ftp://x",`)), nil, "homepage"},
		{"connect path", withManifest(patch(`"entry": "index.html"`, `"entry": "index.html", "connect": ["https://a.example/api"]`)), nil, "web.connect[0]"},
		{"connect slash", withManifest(patch(`"entry": "index.html"`, `"entry": "index.html", "connect": ["https://a.example/"]`)), nil, "web.connect[0]"},
		{"connect http", withManifest(patch(`"entry": "index.html"`, `"entry": "index.html", "connect": ["http://a.example"]`)), nil, "web.connect[0]"},
		{"backend ftp", withManifest(patch(`"author": "Acme",`, `"author": "Acme", "backend": {"url": "ftp://x.example"},`)), nil, "backend.url"},
		{"backend query", withManifest(patch(`"author": "Acme",`, `"author": "Acme", "backend": {"url": "https://x.example/p?a=1"},`)), nil, "query or fragment"},
		{"backend userinfo", withManifest(patch(`"author": "Acme",`, `"author": "Acme", "backend": {"url": "https://u:p@x.example"},`)), nil, "user info"},
		{"seed array", withManifest(patch(`"label": "Task list"}]`, `"label": "Task list", `+strings.Replace(seedObj, "%s", `[1]`, 1))), nil, "seed[0].data must be a JSON object"},
		{"seed null", withManifest(patch(`"label": "Task list"}]`, `"label": "Task list", `+strings.Replace(seedObj, "%s", `null`, 1))), nil, "seed[0].data must be a JSON object"},
		{"seed too big", withManifest(patch(`"label": "Task list"}]`, `"label": "Task list", `+strings.Replace(seedObj, "%s", `{"a":"`+strings.Repeat("x", MaxRecordBytes)+`"}`, 1))), nil, "at most 65536 bytes"},
		{"bad collection", withManifest(patch(`"label": "Task list"}]`, `"label": "Task list", "seed":[{"collection":"Items","data":{}}]}]`)), nil, `collection "Items"`},
		{"bad verb", withManifest(patch(`"author": "Acme",`, `"author": "Acme", "notifications": {"Bad Verb": "x"},`)), nil, `verb "Bad Verb"`},
		{"long template", withManifest(patch(`"author": "Acme",`, `"author": "Acme", "notifications": {"a": "`+strings.Repeat("t", 201)+`"},`)), nil, "notifications.a"},
		{"bad locale", withManifest(patch(`"author": "Acme",`, `"author": "Acme", "translations": {"english": {"a": "b"}},`)), nil, `locale "english"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := tc.raw
			if data == nil {
				data = buildZip(t, tc.entries)
			}
			_, err := Read(data)
			var pe *Error
			if !errors.As(err, &pe) {
				t.Fatalf("want *Error, got %v", err)
			}
			if !strings.Contains(pe.Msg, tc.want) {
				t.Fatalf("error %q does not contain %q", pe.Msg, tc.want)
			}
		})
	}
}

func TestValidators(t *testing.T) {
	if !ValidCollection("items_2") || ValidCollection("2items") || ValidCollection("") {
		t.Fatal("collection")
	}
	if !ValidVerb("task.assigned-x") || ValidVerb("Bad") {
		t.Fatal("verb")
	}
	if ContentType("a/b.WASM") != "application/wasm" || ContentType("x.bin") != "application/octet-stream" {
		t.Fatal("content type")
	}
}
