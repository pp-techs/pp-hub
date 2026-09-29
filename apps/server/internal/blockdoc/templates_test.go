package blockdoc

import (
	"errors"
	"testing"

	"github.com/nhatbui/pp-hub/apps/server/internal/mdoc"
)

// A template is where a document starts, so it must open editable (not
// lossy), and survive the editor untouched: its markdown is what mdoc.Render
// writes for what mdoc.Parse reads from it, or the first save would rewrite a
// document nobody edited.
func TestTemplatesAreCanonicalInEveryLanguage(t *testing.T) {
	for _, locale := range []string{"en", "vi"} {
		list := Templates(locale)
		if len(list) != len(templateOrder) {
			t.Fatalf("%s: %d templates, want %d", locale, len(list), len(templateOrder))
		}
		for _, tpl := range list {
			if tpl.Name == "" || tpl.Description == "" {
				t.Errorf("%s/%s: missing name or description", locale, tpl.ID)
			}
			src, err := TemplateContent(tpl.ID, locale)
			if err != nil {
				t.Fatalf("%s/%s: %v", locale, tpl.ID, err)
			}
			parsed, err := mdoc.Parse(src)
			if err != nil {
				t.Fatalf("%s/%s: parse: %v", locale, tpl.ID, err)
			}
			if parsed.Lossy {
				t.Errorf("%s/%s: lossy", locale, tpl.ID)
			}
			if out := string(mdoc.Render(parsed.Doc, parsed.Frontmatter)); out != string(src) {
				t.Errorf("%s/%s: not in canonical form; mdoc.Render writes:\n%s", locale, tpl.ID, out)
			}
		}
	}
}

func TestTemplateLanguageAndLookup(t *testing.T) {
	for locale, want := range map[string]string{"vi": "vi", "vi-VN": "vi", "VI": "vi", "en-US": "en", "fr": "en", "": "en"} {
		if got := TemplateLanguage(locale); got != want {
			t.Errorf("TemplateLanguage(%q) = %q, want %q", locale, got, want)
		}
	}
	en, _ := TemplateContent("meeting-notes", "en-GB")
	vi, _ := TemplateContent("meeting-notes", "vi-VN")
	if string(en) == string(vi) {
		t.Fatal("the Vietnamese template is the English one")
	}
	for _, id := range []string{"nope", "../en/outline", ""} {
		if _, err := TemplateContent(id, "en"); !errors.Is(err, ErrUnknownTemplate) {
			t.Errorf("TemplateContent(%q): %v, want ErrUnknownTemplate", id, err)
		}
	}
}
