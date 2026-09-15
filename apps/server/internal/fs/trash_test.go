package fs

import "testing"

// A restore that collides with a live sibling comes back under a suffixed
// name. The extension has to survive the suffix, or the restored file stops
// opening in the application that wrote it.
func TestSuffixName(t *testing.T) {
	cases := []struct {
		name string
		n    int
		want string
	}{
		{"report.pdf", 2, "report (2).pdf"},
		{"report.pdf", 10, "report (10).pdf"},
		{"archive.tar.gz", 2, "archive.tar (2).gz"},
		{"Invoices", 2, "Invoices (2)"},
		{".gitignore", 2, ".gitignore (2)"},
		{"report (2).pdf", 3, "report (2) (3).pdf"},
	}
	for _, c := range cases {
		if got := suffixName(c.name, c.n); got != c.want {
			t.Errorf("suffixName(%q, %d) = %q, want %q", c.name, c.n, got, c.want)
		}
	}
}
