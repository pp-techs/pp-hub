package mdoc

import (
	"reflect"
	"strings"
	"testing"
)

func TestRelativePath(t *testing.T) {
	for ref, want := range map[string][]string{
		"a.png":                        {"a.png"},
		"./images/a.png":               {"images", "a.png"},
		"../x/./y.pdf":                 {"..", "x", "y.pdf"},
		"../../a.png":                  {"..", "..", "a.png"},
		"a/../b.png":                   {"b.png"},
		"a/../../b.png":                {"..", "b.png"},
		"images//a.png":                {"images", "a.png"},
		"my%20photo%20(1).png":         {"my photo (1).png"},
		"my photo.png":                 {"my photo.png"},
		"Bu\u0300i.png":                {"Bùi.png"}, // decomposed → composed
		"B%C3%B9i.png":                 {"Bùi.png"},
		"a.png?v=2#frag":               {"a.png"},
		"./a:b.png":                    {"a:b.png"},
		"a+b.png":                      {"a+b.png"},
		"Spec.md#scope":                {"Spec.md"},
		strings.Repeat("a/", 31) + "b": append(repeat("a", 31), "b"),
	} {
		got, ok := RelativePath(ref)
		if !ok || !reflect.DeepEqual(got, want) {
			t.Errorf("RelativePath(%q) = %q, %v; want %q", ref, got, ok, want)
		}
	}
	for _, ref := range []string{
		"", "/a.png", `\a.png`, "//cdn.example/a.png",
		"https://example.com/a.png", "attachment:abc", "data:image/png;base64,xx", "mailto:a@b.c",
		"C:\\a.png", "#heading", "?q=1", "images/", "images/.", "images/..", ".", "..", "a/b/../..",
		"a%2Fb.png", "a%5Cb.png", "bad%zz.png",
		strings.Repeat("a/", 32) + "b", strings.Repeat("x", 1025),
	} {
		if got, ok := RelativePath(ref); ok {
			t.Errorf("RelativePath(%q) = %q, want no relative path", ref, got)
		}
	}
}

func repeat(s string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = s
	}
	return out
}

// Media sources, posters and link targets that are relative are found and
// rewritten; anything else — attachments, URLs, embeds — is left alone.
func TestRelativeRefsAndMapRelative(t *testing.T) {
	parsed, err := Parse([]byte("![a](images/a.png)\n\n![r](https://x.example/r.png)\n\n" +
		"<!-- slash:video {\"poster\":\"p.png\"} -->\n[clip](clip.mp4)\n\n" +
		"See [spec](../Spec.md#scope), [site](https://x.example) and [again](images/a.png).\n\n" +
		"![att](attachment:abc)\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"images/a.png", "clip.mp4", "p.png", "../Spec.md#scope"}
	if got := RelativeRefs(parsed.Doc); !reflect.DeepEqual(got, want) {
		t.Fatalf("RelativeRefs = %q, want %q", got, want)
	}
	mapped := MapRelative(parsed.Doc, func(ref string) (string, bool) {
		if ref == "p.png" {
			return "", false
		}
		return "assets/" + ref, true
	})
	out := string(Render(mapped, parsed.Frontmatter))
	for _, s := range []string{"](assets/images/a.png)", "](assets/clip.mp4)", `"poster":"p.png"`,
		"](assets/../Spec.md#scope)", "](https://x.example/r.png)", "](https://x.example)", "](attachment:abc)"} {
		if !strings.Contains(out, s) {
			t.Errorf("mapped markdown lacks %q:\n%s", s, out)
		}
	}
	if again := string(Render(parsed.Doc, parsed.Frontmatter)); strings.Contains(again, "assets/") {
		t.Fatalf("MapRelative changed the original:\n%s", again)
	}
}
