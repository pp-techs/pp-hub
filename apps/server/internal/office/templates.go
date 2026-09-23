package office

import (
	"embed"
	"fmt"
	"strings"
)

//go:embed templates/*/new.docx templates/*/new.xlsx templates/*/new.pptx
var templatesFS embed.FS

type Kind string

const (
	KindDocument     Kind = "document"
	KindSpreadsheet  Kind = "spreadsheet"
	KindPresentation Kind = "presentation"
)

func ParseKind(s string) (Kind, bool) {
	switch Kind(s) {
	case KindDocument:
		return KindDocument, true
	case KindSpreadsheet:
		return KindSpreadsheet, true
	case KindPresentation:
		return KindPresentation, true
	default:
		return "", false
	}
}

func (k Kind) Ext() string {
	switch k {
	case KindDocument:
		return ".docx"
	case KindSpreadsheet:
		return ".xlsx"
	case KindPresentation:
		return ".pptx"
	default:
		return ""
	}
}

// Template returns the blank file for kind in the caller's language:
// a locale starting "vi" gets vi-VN, everything else en-US.
func Template(k Kind, locale string) ([]byte, error) {
	if _, ok := ParseKind(string(k)); !ok {
		return nil, fmt.Errorf("unknown document kind %q", k)
	}

	loc := "en-US"
	if strings.HasPrefix(strings.ToLower(locale), "vi") {
		loc = "vi-VN"
	}

	path := fmt.Sprintf("templates/%s/new%s", loc, k.Ext())
	return templatesFS.ReadFile(path)
}
