package checklist

import "testing"

func TestManifestMatchesConstants(t *testing.T) {
	m := New().Manifest()
	if m.ID != "checklist" || m.Kind(ListKind) == nil || len(m.NodeKinds) != 1 {
		t.Fatalf("manifest %+v disagrees with ListKind %q", m, ListKind)
	}
	if m.Kind(ListKind).OnCopy == nil || m.Migrations == nil {
		t.Fatal("hooks or migrations not attached")
	}
}
