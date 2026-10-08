package exthost

import (
	"strings"
	"testing"

	"github.com/nhatbui/pp-hub/apps/server/extsdk"
)

type stubExt struct{ m extsdk.Manifest }

func (s stubExt) Manifest() extsdk.Manifest  { return s.m }
func (s stubExt) Register(extsdk.Host) error { return nil }

func manifest(id string, kinds ...string) extsdk.Manifest {
	m := extsdk.Manifest{ID: id, Name: id, Version: "1.0.0", SDK: extsdk.APIVersion}
	for _, k := range kinds {
		m.NodeKinds = append(m.NodeKinds, extsdk.NodeKind{Kind: k})
	}
	return m
}

func TestNewRefusesBadExtensions(t *testing.T) {
	wrongSDK := manifest("a", "a.x")
	wrongSDK.SDK = extsdk.APIVersion + 1
	noVersion := manifest("a", "a.x")
	noVersion.Version = ""
	tests := []struct {
		name string
		exts []extsdk.Ext
		want string
	}{
		{"ok", []extsdk.Ext{stubExt{manifest("a", "a.x")}, stubExt{manifest("b", "b.x")}}, ""},
		{"sdk version", []extsdk.Ext{stubExt{wrongSDK}}, "extension API"},
		{"bad id", []extsdk.Ext{stubExt{manifest("A", "A.x")}}, "must match"},
		{"invalid manifest", []extsdk.Ext{stubExt{noVersion}}, "invalid manifest"},
		{"kind prefix", []extsdk.Ext{stubExt{manifest("a", "b.x")}}, "must be"},
		{"duplicate id", []extsdk.Ext{stubExt{manifest("a")}, stubExt{manifest("a")}}, "twice"},
		{"duplicate kind", []extsdk.Ext{stubExt{manifest("a", "a.x", "a.x")}}, "twice"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(Config{}, tc.exts...)
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want containing %q", err, tc.want)
			}
		})
	}
}
