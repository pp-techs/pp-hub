package extsdk_test

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nhatbui/pp-hub/apps/server/extsdk"
)

// An extension is written against extsdk and nothing else of the server. Go
// cannot say so itself — ext/ and internal/ share a module — so this walks the
// imports of every extension file, and of the SDK itself, and fails on any path
// through internal/. The boundary is a test, not a convention.
func TestExtImportsOnlySDK(t *testing.T) {
	for _, root := range []string{".", "../ext"} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
				return err
			}
			f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			for _, imp := range f.Imports {
				p := strings.Trim(imp.Path.Value, `"`)
				if strings.Contains(p, "/internal/") || strings.HasSuffix(p, "/internal") {
					t.Errorf("%s imports %s: extensions may use extsdk only", path, p)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// An extension directory is a plugin package: it carries a valid pp-app.json
// whose server.package is the Go import path of the directory itself.
func TestExtDirsCarryManifest(t *testing.T) {
	const module = "github.com/nhatbui/pp-hub/apps/server"
	dirs, err := os.ReadDir("../ext")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		n++
		data, err := os.ReadFile(filepath.Join("../ext", d.Name(), "pp-app.json"))
		if err != nil {
			t.Errorf("ext/%s: %v", d.Name(), err)
			continue
		}
		m, err := extsdk.ParseManifest(data)
		if err != nil {
			t.Errorf("ext/%s: %v", d.Name(), err)
			continue
		}
		var raw struct {
			Server struct {
				Package string `json:"package"`
			} `json:"server"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatal(err)
		}
		if want := module + "/ext/" + d.Name(); raw.Server.Package != want {
			t.Errorf("ext/%s: server.package = %q, want %q", d.Name(), raw.Server.Package, want)
		}
		if m.ID != d.Name() {
			t.Errorf("ext/%s: id = %q, want the directory name", d.Name(), m.ID)
		}
	}
	if n == 0 {
		t.Fatal("no extensions found under ../ext")
	}
}
