package kanban

import "testing"

func TestManifestMatchesConstants(t *testing.T) {
	m := New().Manifest()
	if m.ID != "kanban" || m.Kind(BoardKind) == nil || len(m.NodeKinds) != 1 {
		t.Fatalf("manifest %+v disagrees with BoardKind %q", m, BoardKind)
	}
	k := m.Kind(BoardKind)
	if k.OnCreate == nil || k.OnCopy == nil || k.ValidSubject == nil || m.Migrations == nil {
		t.Fatal("hooks or migrations not attached")
	}
}
