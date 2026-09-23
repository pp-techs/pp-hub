package office

import (
	"archive/zip"
	"bytes"
	"testing"
)

func TestTemplates(t *testing.T) {
	kinds := []Kind{KindDocument, KindSpreadsheet, KindPresentation}
	locales := []string{"vi", "en", ""}

	for _, k := range kinds {
		for _, loc := range locales {
			t.Run(string(k)+"_"+loc, func(t *testing.T) {
				data, err := Template(k, loc)
				if err != nil {
					t.Fatalf("Template(%q, %q) returned error: %v", k, loc, err)
				}
				if len(data) == 0 {
					t.Fatalf("Template(%q, %q) returned empty data", k, loc)
				}

				zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
				if err != nil {
					t.Fatalf("zip.NewReader failed: %v", err)
				}

				hasContentTypes := false
				for _, f := range zr.File {
					if f.Name == "[Content_Types].xml" {
						hasContentTypes = true
						break
					}
				}
				if !hasContentTypes {
					t.Fatalf("zip archive missing [Content_Types].xml for %s (locale: %q)", k, loc)
				}
			})
		}
	}
}

func TestParseKindAndExt(t *testing.T) {
	k, ok := ParseKind("document")
	if !ok || k != KindDocument || k.Ext() != ".docx" {
		t.Errorf("expected KindDocument .docx, got %v (%v)", k, ok)
	}

	k, ok = ParseKind("spreadsheet")
	if !ok || k != KindSpreadsheet || k.Ext() != ".xlsx" {
		t.Errorf("expected KindSpreadsheet .xlsx, got %v (%v)", k, ok)
	}

	k, ok = ParseKind("presentation")
	if !ok || k != KindPresentation || k.Ext() != ".pptx" {
		t.Errorf("expected KindPresentation .pptx, got %v (%v)", k, ok)
	}

	_, ok = ParseKind("pdf")
	if ok {
		t.Error("expected ParseKind(pdf) to return false")
	}
}
