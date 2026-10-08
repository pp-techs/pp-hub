package extsdk_test

import (
	"strings"
	"testing"

	"github.com/nhatbui/pp-hub/apps/server/extsdk"
)

const okManifest = `{"id":"demo","name":"Demo","version":"1.2.3","description":"d","author":"a",
"engines":{"sdk":1},"nodeKinds":[{"kind":"demo.thing"}],
"server":{"package":"x/demo"},"web":{"package":"@x/demo"}}`

func TestParseManifest(t *testing.T) {
	rep := func(old, new string) string { return strings.Replace(okManifest, old, new, 1) }
	tests := []struct {
		name, in, wantErr string
	}{
		{"ok", okManifest, ""},
		{"optional fields", rep(`"author":"a"`, `"author":"a","license":"MIT","homepage":"https://x"`), ""},
		{"unknown field", rep(`"name"`, `"extra":1,"name"`), "unknown field"},
		{"not json", `nope`, "pp-app.json"},
		{"trailing data", okManifest + `{}`, "trailing"},
		{"bad id", rep(`"id":"demo"`, `"id":"Demo"`), "id"},
		{"id too long", rep(`"id":"demo"`, `"id":"a`+strings.Repeat("b", 32)+`"`), "id"},
		{"no name", rep(`"name":"Demo"`, `"name":" "`), "name"},
		{"bad semver", rep(`1.2.3`, `1.2`), "version"},
		{"no sdk", rep(`"engines":{"sdk":1}`, `"engines":{}`), "engines.sdk"},
		{"sdk zero", rep(`"sdk":1`, `"sdk":0`), "engines.sdk"},
		{"kind prefix", rep(`demo.thing`, `other.thing`), "node kind"},
		{"kind empty name", rep(`demo.thing`, `demo.`), "node kind"},
		{"kind twice", rep(`[{"kind":"demo.thing"}]`, `[{"kind":"demo.thing"},{"kind":"demo.thing"}]`), "twice"},
		{"no server", rep(`"server":{"package":"x/demo"}`, `"server":{}`), "server.package"},
		{"no web", rep(`"web":{"package":"@x/demo"}`, `"web":{}`), "web.package"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m, err := extsdk.ParseManifest([]byte(tc.in))
			if tc.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
				if m.ID != "demo" || m.Version != "1.2.3" || m.SDK != 1 || len(m.NodeKinds) != 1 || m.Kind("demo.thing") == nil {
					t.Fatalf("unexpected manifest %+v", m)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestManifestKindPointsIntoKinds(t *testing.T) {
	m := extsdk.MustManifest([]byte(okManifest))
	m.Kind("demo.thing").OnCopy = nil
	if m.Kind("demo.missing") != nil {
		t.Fatal("unknown kind must be nil")
	}
	k := m.Kind("demo.thing")
	k.ValidSubject = nil
	if m.NodeKinds[0].Kind != "demo.thing" || k != &m.NodeKinds[0] {
		t.Fatal("Kind must point into NodeKinds")
	}
}

func TestMustManifestPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	extsdk.MustManifest([]byte(`{}`))
}
