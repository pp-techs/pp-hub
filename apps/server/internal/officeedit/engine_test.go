package officeedit

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nhatbui/pp-hub/apps/server/internal/office"
)

func realRunner(t *testing.T) *runner {
	t.Helper()
	binary, err := exec.LookPath("officecli")
	if err != nil {
		t.Skip("officecli not installed; real OfficeCLI integration coverage requires the pinned binary")
	}
	binary, err = filepath.Abs(binary)
	if err != nil {
		t.Fatal(err)
	}
	return &runner{binary: binary, slots: make(chan struct{}, 2)}
}

func fixture(t *testing.T, e *runner, format string) []byte {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "document."+format)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := e.command(ctx, dir, nil, "create", file, "--json"); err != nil {
		t.Fatalf("create %s fixture: %v", format, err)
	}
	data, err := readFile(file, MaxFileBytes)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func edit(t *testing.T, e *runner, format string, content []byte, ops ...Operation) Response {
	t.Helper()
	result, err := e.Do(context.Background(), Request{Action: "edit", Format: format, Content: content, Operations: ops})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Content) == 0 || !bytes.Contains(result.Data, []byte(`"validated":true`)) {
		t.Fatalf("edit returned no validated Office file: %s", result.Data)
	}
	return result
}

func read(t *testing.T, e *runner, format string, content []byte, path string) json.RawMessage {
	t.Helper()
	result, err := e.Do(context.Background(), Request{Action: "read", Format: format, Content: content, Path: path, Depth: 8})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Content) != 0 || !json.Valid(result.Data) {
		t.Fatal("read must return only structured JSON")
	}
	return result.Data
}

func textPath(data json.RawMessage, want string) string {
	var tree any
	_ = json.Unmarshal(data, &tree)
	var find func(any) string
	find = func(v any) string {
		switch x := v.(type) {
		case map[string]any:
			if text, _ := x["text"].(string); text == want {
				if p, _ := x["path"].(string); p != "" && p != "/" && p != "/body" {
					return p
				}
			}
			for _, child := range x {
				if p := find(child); p != "" {
					return p
				}
			}
		case []any:
			for _, child := range x {
				if p := find(child); p != "" {
					return p
				}
			}
		}
		return ""
	}
	return find(tree)
}

func packageText(t *testing.T, content []byte) string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	for _, f := range zr.File {
		if !strings.HasSuffix(f.Name, ".xml") {
			continue
		}
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		out.Write(data)
	}
	return out.String()
}

func TestOfficeCLIReadEditRemoveAllFormats(t *testing.T) {
	for _, format := range []string{"docx", "xlsx", "pptx"} {
		t.Run(format, func(t *testing.T) {
			e := realRunner(t)
			original := fixture(t, e, format)
			marker := "OfficeAgentOriginal"
			var ops []Operation
			switch format {
			case "docx":
				ops = []Operation{{Op: "add", Parent: "/body", Type: "paragraph", Props: map[string]string{"text": marker, "bold": "true"}}, {Op: "add", Parent: "/body", Type: "table", Props: map[string]string{"rows": "2", "cols": "2"}}, {Op: "set", Path: "/body/tbl[1]/tr[1]/tc[1]", Props: map[string]string{"text": "TableCell"}}}
			case "xlsx":
				ops = []Operation{{Op: "add", Parent: "/", Type: "sheet", Props: map[string]string{"name": "Data"}}, {Op: "set", Path: "/Data/A1", Props: map[string]string{"value": marker, "bold": "true"}}, {Op: "set", Path: "/Data/A2", Props: map[string]string{"value": "42"}}, {Op: "set", Path: "/Data/B2", Props: map[string]string{"formula": "SUM(A2:A2)", "numberformat": "0.00"}}}
			case "pptx":
				ops = []Operation{{Op: "add", Parent: "/", Type: "slide", Props: map[string]string{"title": "Agent slide"}}, {Op: "add", Parent: "/slide[1]", Type: "shape", Props: map[string]string{"text": marker, "bold": "true", "x": "1cm", "y": "1cm", "width": "5cm", "height": "2cm"}}, {Op: "add", Parent: "/slide[1]", Type: "table", Props: map[string]string{"rows": "2", "cols": "2", "x": "1cm", "y": "4cm", "width": "8cm", "height": "2cm"}}, {Op: "set", Path: "/slide[1]/table[1]/tr[1]/tc[1]", Props: map[string]string{"text": "TableCell"}}}
			}
			first := edit(t, e, format, original, ops...)
			xml := packageText(t, first.Content)
			if !strings.Contains(xml, marker) {
				t.Fatal("requested text was not persisted in OOXML")
			}
			if format == "xlsx" && !strings.Contains(xml, "SUM(A2:A2)") {
				t.Fatal("formula was not persisted")
			}
			if format != "xlsx" && !strings.Contains(xml, "TableCell") {
				t.Fatal("table cell text was not persisted")
			}
			view := read(t, e, format, first.Content, "/")
			path := textPath(view, marker)
			if path == "" {
				t.Fatalf("structured read did not expose new text: %s", view)
			}
			changed := edit(t, e, format, first.Content, Operation{Op: "set", Path: path, Find: marker, Replace: "OfficeAgentReplaced"})
			xml = packageText(t, changed.Content)
			if strings.Contains(xml, marker) || !strings.Contains(xml, "OfficeAgentReplaced") {
				t.Fatal("literal replacement was not persisted")
			}
			removed := edit(t, e, format, changed.Content, Operation{Op: "remove", Path: path})
			if strings.Contains(packageText(t, removed.Content), "OfficeAgentReplaced") {
				t.Fatal("remove did not remove the requested node")
			}
			if bytes.Contains(original, []byte(marker)) {
				t.Fatal("edit mutated caller input")
			}
		})
	}
}

func TestOfficeCLIRejectsFailedBatchAndMissingText(t *testing.T) {
	e := realRunner(t)
	original := fixture(t, e, "xlsx")
	snapshot := bytes.Clone(original)
	result, err := e.Do(context.Background(), Request{Action: "edit", Format: "xlsx", Content: original, Operations: []Operation{{Op: "set", Path: "/Sheet1/A1", Props: map[string]string{"value": "NeverCommit"}}, {Op: "set", Path: "/MissingSheet/A1", Props: map[string]string{"value": "Bad"}}}})
	if !errors.Is(err, ErrProcessing) || len(result.Content) != 0 {
		t.Fatalf("failed atomic edit returned content: %v", err)
	}
	if !bytes.Equal(snapshot, original) {
		t.Fatal("failed edit mutated the source")
	}
	if strings.Contains(packageText(t, original), "NeverCommit") {
		t.Fatal("failed edit escaped into original")
	}
	_, err = e.Do(context.Background(), Request{Action: "edit", Format: "xlsx", Content: original, Operations: []Operation{{Op: "set", Path: "/Sheet1/A1", Find: "NotPresentAnywhere", Replace: "Replacement"}}})
	if !errors.Is(err, ErrProcessing) {
		t.Fatalf("missing text was silently accepted: %v", err)
	}
}

func TestRequestRejectsUntrustedPowers(t *testing.T) {
	base := Request{Action: "edit", Format: "pptx", Content: []byte("not reached")}
	cases := []Operation{
		{Op: "raw-set", Path: "/slide[1]", Props: map[string]string{"xml": "<evil/>"}},
		{Op: "set", Path: "/slide[1]/shape[1]", Props: map[string]string{"image": "/etc/passwd"}},
		{Op: "add", Parent: "/slide[1]", Type: "picture", Props: map[string]string{"src": "https://example.com"}},
		{Op: "set", Path: "../../etc/passwd", Props: map[string]string{"text": "x"}},
		{Op: "set", Path: "/slide[1]/shape[1]", Props: map[string]string{"bold": "maybe"}},
		{Op: "set", Path: "/slide[1]/shape[1]", Props: map[string]string{"font.color": "red"}},
		{Op: "remove", Path: "/"},
	}
	for _, op := range cases {
		req := base
		req.Operations = []Operation{op}
		if !errors.Is(validateRequest(req), ErrInvalidRequest) {
			t.Fatalf("unsafe operation accepted: %+v", op)
		}
	}
	for _, formula := range []string{`WEBSERVICE("https://example.com")`, `HYPERLINK("file:///etc/passwd")`, `'[external.xlsx]Sheet1'!A1`, `cmd|'/C calc'!A0`, `_xlfn.IMAGE("https://example.com")`} {
		if checkValue(property{Type: "formula"}, formula) {
			t.Fatalf("unsafe formula accepted: %s", formula)
		}
	}
	for _, formula := range []string{"SUM(A1:A10)", "IF(A1>0,ROUND(A1/2,2),0)", "'Other Sheet'!A1+2"} {
		if !checkValue(property{Type: "formula"}, formula) {
			t.Fatalf("local formula rejected: %s", formula)
		}
	}
	for _, path := range []string{"/Data/XFE1", "/Data/A1048577", "/Data/A1:B1000000", "/slide[1]/shape[1] or shape"} {
		format := "xlsx"
		if strings.HasPrefix(path, "/slide") {
			format = "pptx"
		}
		if _, err := pathType(format, path); err == nil {
			t.Fatalf("unbounded/query path accepted: %s", path)
		}
	}
}

func TestOutputWarningsAndCaps(t *testing.T) {
	for _, s := range []string{`{"success":true,"warnings":["ignored property"]}`, `{"success":true,"data":{"summary":{"failed":1}}}`, `{"success":true,"data":{"errors":[{}]}}`, `{"success":true,"data":{"atomicRolledBack":true}}`} {
		var v any
		_ = json.Unmarshal([]byte(s), &v)
		if !hasFailure(v) {
			t.Fatalf("unsafe CLI success envelope accepted: %s", s)
		}
	}
	canceled := false
	w := cappedWriter{limit: 4, cancel: func() { canceled = true }}
	if _, err := w.Write([]byte("12345")); err == nil || !canceled || w.buf.Len() != 4 {
		t.Fatal("subprocess output was not bounded while writing")
	}
}

func TestEmbeddedTemplatesRemainEditable(t *testing.T) {
	for _, kind := range []office.Kind{office.KindDocument, office.KindSpreadsheet, office.KindPresentation} {
		for _, locale := range []string{"en", "vi"} {
			t.Run(string(kind)+"/"+locale, func(t *testing.T) {
				engine := realRunner(t)
				content, err := office.Template(kind, locale)
				if err != nil {
					t.Fatal(err)
				}
				format := strings.TrimPrefix(kind.Ext(), ".")
				const marker = "Nội dung đã cập nhật"
				var ops []Operation
				path := "/"
				switch kind {
				case office.KindDocument:
					ops = []Operation{{Op: "add", Parent: "/body", Type: "paragraph", Props: map[string]string{"text": marker}}}
				case office.KindSpreadsheet:
					var workbook struct {
						Results []struct {
							Children []struct {
								Path string `json:"path"`
							} `json:"children"`
						} `json:"results"`
					}
					if err := json.Unmarshal(read(t, engine, format, content, "/"), &workbook); err != nil {
						t.Fatal(err)
					}
					if len(workbook.Results) != 1 || len(workbook.Results[0].Children) != 1 {
						t.Fatal("expected a single-sheet template")
					}
					path = workbook.Results[0].Children[0].Path + "/A1"
					ops = []Operation{{Op: "set", Path: path, Props: map[string]string{"value": marker}}}
				case office.KindPresentation:
					ops = []Operation{
						{Op: "add", Parent: "/", Type: "slide", Props: map[string]string{"layout": "blank"}},
						{Op: "add", Parent: "/slide[1]", Type: "shape", Props: map[string]string{"text": marker}},
					}
				}
				result := edit(t, engine, format, content, ops...)
				data := read(t, engine, format, result.Content, path)
				if textPath(data, marker) == "" {
					t.Fatalf("the edited template did not expose its saved text: %s", data)
				}
			})
		}
	}
}

func TestExcelLiteralReplacementCannotBecomeAFormula(t *testing.T) {
	engine := realRunner(t)
	original := fixture(t, engine, "xlsx")
	first := edit(t, engine, "xlsx", original, Operation{
		Op: "set", Path: "/Sheet1/A1", Props: map[string]string{"value": "Original"},
	})
	result := edit(t, engine, "xlsx", first.Content, Operation{
		Op: "set", Path: "/Sheet1/A1", Find: "Original", Replace: "=SUM(1,2)",
	})
	data := read(t, engine, "xlsx", result.Content, "/Sheet1/A1")
	var parsed struct {
		Results []struct {
			Text   string `json:"text"`
			Format struct {
				Type    string `json:"type"`
				Formula string `json:"formula"`
			} `json:"format"`
		} `json:"results"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.Results) != 1 || parsed.Results[0].Text != "=SUM(1,2)" ||
		parsed.Results[0].Format.Type != "String" || parsed.Results[0].Format.Formula != "" {
		t.Fatalf("literal replacement was evaluated as a formula: %s", data)
	}
}

func TestCellReplacementBounds(t *testing.T) {
	for _, tc := range []struct {
		name        string
		text        string
		find        string
		replacement string
		want        string
		wantErr     error
	}{
		{
			name: "gigabyte expansion refused before replacement",
			text: strings.Repeat("a", 32767), find: "a", replacement: strings.Repeat("b", 32768),
			wantErr: ErrInvalidRequest,
		},
		{
			name: "ASCII cell limit", text: strings.Repeat("a", 32767), find: "a", replacement: "b",
			want: strings.Repeat("b", 32767),
		},
		{
			name: "UTF16 cell limit exceeds even within byte cap",
			text: "aa", find: "a", replacement: strings.Repeat("b", 16384),
			wantErr: ErrInvalidRequest,
		},
		{
			name: "Unicode byte boundary", text: "x" + strings.Repeat("😀", 8191), find: "x", replacement: "😀",
			want: strings.Repeat("😀", 8192),
		},
		{
			name: "Unicode expansion exceeds byte cap",
			text: "xx" + strings.Repeat("😀", 8191), find: "x", replacement: "😀",
			wantErr: ErrInvalidRequest,
		},
		{
			name: "Unicode byte cap stricter than UTF16",
			text: "é" + strings.Repeat("a", 32765), find: "é", replacement: "😀",
			wantErr: ErrInvalidRequest,
		},
		{
			name: "Unicode exact byte cap with retained text",
			text: "é" + strings.Repeat("a", 32764), find: "é", replacement: "😀",
			want: "😀" + strings.Repeat("a", 32764),
		},
		{
			name: "deletion can shrink an oversized source",
			text: strings.Repeat("a", 65536), find: "aa", replacement: "",
			want: "",
		},
		{
			name: "nonoverlapping matches", text: "aaa", find: "aa", replacement: "b", want: "ba",
		},
		{
			name: "formula-like replacement remains literal",
			text: "Original", find: "Original", replacement: `=WEBSERVICE("https://example.com")`,
			want: `=WEBSERVICE("https://example.com")`,
		},
		{
			name: "missing match", text: "Original", find: "missing", wantErr: ErrProcessing,
		},
		{
			name: "empty find", text: "Original", find: "", wantErr: ErrInvalidRequest,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := replaceCellString(tc.text, tc.find, tc.replacement)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("replacement error = %v, want %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("unexpected replacement result (bytes = %d, want %d)", len(got), len(tc.want))
			}
		})
	}
	if got := utf16Length("Aé😀"); got != 4 {
		t.Fatalf("UTF16 length = %d, want 4", got)
	}
}

func TestExcelCellValueLength(t *testing.T) {
	for _, tc := range []struct {
		name    string
		value   string
		wantErr error
	}{
		{name: "maximum ASCII", value: strings.Repeat("a", 32767)},
		{name: "ASCII exceeds cell limit", value: strings.Repeat("a", 32768), wantErr: ErrInvalidRequest},
		{name: "Unicode at byte cap", value: strings.Repeat("😀", 8192)},
		{name: "Unicode exceeds byte cap", value: strings.Repeat("😀", 8193), wantErr: ErrInvalidRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := Request{
				Action: "edit", Format: "xlsx", Content: []byte("not reached"),
				Operations: []Operation{{Op: "set", Path: "/Sheet1/A1", Props: map[string]string{"value": tc.value}}},
			}
			if err := validateRequest(req); !errors.Is(err, tc.wantErr) {
				t.Fatalf("cell value validation = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestSafeFormulaQuotedTokens(t *testing.T) {
	for _, formula := range []string{
		`IF(A1="Pending (review)",1,0)`,
		`'Budget (2026)'!A1`,
		`'Director''s Budget (2026)'!A1`,
		`'WEBSERVICE (Budget)'!A1`,
		`IF(A1="Say ""WEBSERVICE("" now",1,0)`,
		`IF(A1="'Budget (2026)'",1,0)`,
		`IF(A1="Ready ""(review)""",SUM(B1:B2),0)`,
	} {
		if !checkValue(property{Type: "formula"}, formula) {
			t.Errorf("safe quoted formula rejected: %s", formula)
		}
	}
	for _, formula := range []string{
		`IF(A1="Pending (review)",WEBSERVICE("safe"),0)`,
		`IF(A1="Say ""(review)""",_xlfn.IMAGE("safe"),0)`,
		`'Budget (2026)'!A1+HYPERLINK("safe")`,
		`'Safe'' [external.xlsx] Budget (2026)'!A1`,
		`'\\server\share\Budget (2026)'!A1`,
		`'cmd|''/C calc'''!A0`,
		`'cmd''s Budget (2026)'|'/C calc'!A0`,
		`IF(A1="[external.xlsx]Sheet1",1,0)`,
		`IF(A1="https://example.com",1,0)`,
		`IF(A1="Pending (review),1,0)`,
		`'Budget (2026)!A1`,
	} {
		if checkValue(property{Type: "formula"}, formula) {
			t.Errorf("unsafe or unterminated quoted formula accepted: %s", formula)
		}
	}
}

func TestExcelReplacementRejectsOversizedCell(t *testing.T) {
	engine := realRunner(t)
	original := fixture(t, engine, "xlsx")
	first := edit(t, engine, "xlsx", original, Operation{
		Op: "set", Path: "/Sheet1/A1", Props: map[string]string{"value": strings.Repeat("a", 32767)},
	})
	result, err := engine.Do(context.Background(), Request{
		Action: "edit", Format: "xlsx", Content: first.Content,
		Operations: []Operation{{Op: "set", Path: "/Sheet1/A1", Find: "a", Replace: strings.Repeat("b", 32768)}},
	})
	if !errors.Is(err, ErrInvalidRequest) || len(result.Content) != 0 {
		t.Fatalf("oversized replacement returned content or wrong error: content bytes=%d, error=%v", len(result.Content), err)
	}
}
