package officeedit

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type property struct {
	Type   string   `json:"type"`
	Values []string `json:"values,omitempty"`
	Add    bool     `json:"add"`
	Set    bool     `json:"set"`
}
type element struct {
	Properties map[string]property `json:"properties"`
	Parents    []string            `json:"parents,omitempty"`
}

// Only these properties can reach OfficeCLI. In particular no file, image,
// relationship, XML, import, plugin or arbitrary attribute properties exist.
var formats = buildFormats()

func buildFormats() map[string]map[string]element {
	out := make(map[string]map[string]element)
	common := "text:string font:string size:length bold:bool italic:bool underline:string color:color"
	for _, f := range []string{"docx", "xlsx", "pptx"} {
		out[f] = map[string]element{"document": {Properties: props("title:string author:string description:string keywords:string lastModifiedBy:string", false, true)}}
	}
	add := func(f, typ, ps string, parents ...string) {
		out[f][typ] = element{Properties: props(ps, true, true), Parents: parents}
	}
	add("docx", "body", "")
	add("docx", "paragraph", common+" style:string align:enum=left,center,right,justify,both,distribute direction:enum=ltr,rtl spaceBefore:length spaceAfter:length lineSpacing:length listStyle:enum=bullet,ordered,none keepNext:bool keepLines:bool pageBreakBefore:bool indent:length", "body", "cell")
	add("docx", "run", common+" highlight:color strike:bool direction:enum=ltr,rtl", "paragraph")
	add("docx", "table", "width:length style:string align:enum=left,center,right layout:enum=fixed,autofit direction:enum=ltr,rtl", "body", "cell")
	add("docx", "row", "height:length header:bool cantSplit:bool", "table")
	add("docx", "cell", "text:string width:length direction:enum=ltr,rtl nowrap:bool", "row")
	extend(out["docx"], "cell", common+" fill:color align:enum=left,center,right,justify valign:enum=top,center,bottom", false, true)
	extend(out["docx"], "document", "docDefaults.font:string docDefaults.fontSize:length evenAndOddHeaders:bool", false, true)
	add("xlsx", "sheet", "name:sheetname hidden:bool tabColor:color", "document")
	extend(out["xlsx"], "sheet", "direction:enum=ltr,rtl gridlines:bool headings:bool zoom:positive", false, true)
	add("xlsx", "cell", "value:string formula:formula type:enum=string,number,boolean,date,error numberformat:string font:string size:length bold:bool italic:bool font.color:color fill:color underline:enum=single,double,singleAccounting,doubleAccounting,none alignment.horizontal:enum=left,center,right,justify,fill,distributed alignment.vertical:enum=top,center,bottom alignment.wrapText:bool alignment.readingOrder:enum=context,ltr,rtl clear:bool", "sheet", "cell")
	add("xlsx", "row", "height:length hidden:bool", "sheet")
	add("xlsx", "col", "width:length hidden:bool", "sheet")
	extend(out["xlsx"], "col", "name:column", true, false)
	add("xlsx", "table", "name:string ref:range headerRow:bool", "sheet")
	extend(out["xlsx"], "document", "calc.mode:enum=auto,manual,autoExceptTables calc.fullPrecision:bool", false, true)
	add("pptx", "slide", "background:color hidden:bool name:string layout:string", "document")
	extend(out["pptx"], "slide", "title:string text:string", true, false)
	extend(out["pptx"], "slide", "notes:string", false, true)
	add("pptx", "shape", common+" name:string x:length y:length width:length height:length fill:color rotation:number geometry:enum=rect,roundRect,ellipse,triangle,diamond,parallelogram,rightArrow,star5 align:enum=left,center,right,justify valign:enum=top,center,bottom direction:enum=ltr,rtl", "slide")
	add("pptx", "paragraph", "text:string align:enum=left,center,right,justify bold:bool italic:bool color:color size:length lineSpacing:length spaceBefore:length spaceAfter:length", "shape", "cell")
	add("pptx", "run", common, "paragraph")
	add("pptx", "table", "x:length y:length width:length height:length", "slide")
	add("pptx", "row", "height:length", "table")
	add("pptx", "cell", "fill:color valign:enum=top,center,bottom", "row")
	extend(out["pptx"], "cell", common+" align:enum=left,center,right,justify", false, true)
	extend(out["pptx"], "document", "slideWidth:length slideHeight:length show.loop:bool", false, true)
	for _, f := range []string{"docx", "pptx"} {
		extend(out[f], "table", "rows:count cols:count", true, false)
		extend(out[f], "row", "cols:count", true, false)
	}
	extend(out["xlsx"], "row", "cols:count", true, false)
	return out
}

func props(spec string, add, set bool) map[string]property {
	out := make(map[string]property)
	for field := range strings.FieldsSeq(spec) {
		key, kind, _ := strings.Cut(field, ":")
		kind, values, _ := strings.Cut(kind, "=")
		p := property{Type: kind, Add: add, Set: set}
		if values != "" {
			p.Values = strings.Split(values, ",")
		}
		out[key] = p
	}
	return out
}
func extend(es map[string]element, typ, spec string, add, set bool) {
	e := es[typ]
	for k, p := range props(spec, add, set) {
		if old, ok := e.Properties[k]; ok {
			p.Add = p.Add || old.Add
			p.Set = p.Set || old.Set
		}
		e.Properties[k] = p
	}
	es[typ] = e
}

// Schema describes this service's restricted interface, not OfficeCLI's raw CLI.
func Schema(format string) (json.RawMessage, error) {
	es, ok := formats[format]
	if !ok {
		return nil, fmt.Errorf("%w: format must be docx, xlsx or pptx", ErrInvalidRequest)
	}
	return json.Marshal(struct {
		Format           string             `json:"format"`
		Operations       []string           `json:"operations"`
		Elements         map[string]element `json:"elements"`
		FormulaFunctions []string           `json:"formula_functions,omitempty"`
		PathExamples     []string           `json:"path_examples"`
		Limits           map[string]int     `json:"limits"`
		Notes            []string           `json:"notes"`
	}{
		Format: format, Operations: []string{"set", "add", "remove"}, Elements: es,
		FormulaFunctions: strings.Fields(allowedFormulaFunctions),
		PathExamples:     map[string][]string{"docx": {"/", "/body", "/body/p[1]", "/body/p[@paraId=1A2B3C4D]/r[1]", "/body/tbl[1]/tr[1]/tc[1]"}, "xlsx": {"/", "/Sheet1", "/Sheet1/A1", "/Sheet1/A1:D10", "/Sheet1/row[1]", "/Sheet1/col[A]"}, "pptx": {"/", "/slide[1]", "/slide[1]/shape[@id=2]", "/slide[1]/shape[1]/paragraph[1]/run[1]", "/slide[1]/table[1]/tr[1]/tc[1]"}}[format],
		Limits:           map[string]int{"file_bytes": MaxFileBytes, "operations": MaxOperations, "depth": MaxDepth, "path_bytes": MaxPathBytes, "operations_bytes": MaxOperationsBytes, "range_cells": 10000},
		Notes: []string{
			"Paths are absolute document paths, never filesystem paths. Indices are 1-based; stable @id, @paraId and @name paths are accepted.",
			"set uses path and props; add uses parent, type and props; remove uses path. Root removal and arbitrary XML attributes are forbidden. Excel cell ranges are read-only and limited to 10000 cells.",
			"set may use literal find/replace on docx/pptx text paths and xlsx string cells; use value/formula properties for numeric or formula cells. No regular expressions. A find with formatting props and an empty replace formats matches without deleting text; an empty replace with no props deletes matches. Missing matches fail the request.",
			"Formulas allow local arithmetic, references and formula_functions only. No external workbook references, DDE or network-capable functions.",
			"Editing is atomic. Any command warning, command failure or OpenXML validation error rejects the entire result. Files with macros, embedded objects, external relationships other than ordinary http/https/mailto hyperlinks, or unsafe ZIP entries are refused.",
		},
	})
}

var (
	lengthPattern  = regexp.MustCompile(`^-?(?:[0-9]{1,8}(?:\.[0-9]{1,4})?)(?:pt|cm|mm|in|px|emu|x|%)?$`)
	colorPattern   = regexp.MustCompile(`^(?:#?[0-9A-Fa-f]{6}|[A-Za-z][A-Za-z0-9]{0,24})$`)
	columnPattern  = regexp.MustCompile(`^[A-Z]{1,3}$`)
	sheetPattern   = regexp.MustCompile(`^sheet\[[1-9][0-9]{0,3}\]$`)
	cellPattern    = regexp.MustCompile(`^([A-Z]{1,3})([1-9][0-9]{0,6})$`)
	segmentPattern = regexp.MustCompile(`^([A-Za-z]+)(?:\[([1-9][0-9]{0,6}|@(id|paraId|name)=([^\[\]/]+))\])?$`)
)

const (
	maxPropertyValueBytes = 32768
	maxCellUTF16Units     = 32767
)

func checkValue(p property, v string) bool {
	if !utf8.ValidString(v) || len(v) > maxPropertyValueBytes || strings.IndexFunc(v, func(r rune) bool { return r < 32 && r != '\n' && r != '\r' && r != '\t' }) >= 0 {
		return false
	}
	switch p.Type {
	case "bool":
		return v == "true" || v == "false"
	case "enum":
		for _, x := range p.Values {
			if x == v {
				return true
			}
		}
		return false
	case "count":
		n, err := strconv.Atoi(v)
		return err == nil && n > 0 && n <= 100
	case "number", "positive":
		n, err := strconv.ParseFloat(v, 64)
		return err == nil && !math.IsNaN(n) && !math.IsInf(n, 0) && math.Abs(n) <= 1000000 && (p.Type != "positive" || n > 0)
	case "length":
		return lengthPattern.MatchString(v)
	case "color":
		return colorPattern.MatchString(v)
	case "sheetname":
		return len(v) > 0 && utf8.RuneCountInString(v) <= 31 && !strings.ContainsAny(v, "[]:*?/\\") && v != "." && v != ".." && !strings.HasPrefix(v, "-")
	case "column":
		return columnPattern.MatchString(v) && columnNumber(v) <= 16384
	case "range":
		parts := strings.Split(v, ":")
		if len(parts) != 2 || !validCell(parts[0]) || !validCell(parts[1]) {
			return false
		}
		a := cellPattern.FindStringSubmatch(parts[0])
		b := cellPattern.FindStringSubmatch(parts[1])
		ar, _ := strconv.Atoi(a[2])
		br, _ := strconv.Atoi(b[2])
		width := columnNumber(b[1]) - columnNumber(a[1]) + 1
		height := br - ar + 1
		return width > 0 && height > 0 && width*height <= 10000
	case "formula":
		return safeFormula(v)
	default:
		return true
	}
}

const allowedFormulaFunctions = " SUM AVERAGE MIN MAX COUNT COUNTA COUNTBLANK COUNTIF COUNTIFS SUMIF SUMIFS AVERAGEIF AVERAGEIFS IF IFERROR IFNA AND OR NOT TRUE FALSE ROUND ROUNDUP ROUNDDOWN ABS MOD INT CEILING FLOOR SQRT POWER LEN LEFT RIGHT MID TRIM LOWER UPPER CONCAT CONCATENATE TEXT VALUE DATE YEAR MONTH DAY TODAY NOW VLOOKUP HLOOKUP XLOOKUP INDEX MATCH ISBLANK ISNUMBER ISTEXT SUBTOTAL SUMPRODUCT "

func safeFormula(v string) bool {
	if len(v) > 4096 || strings.ContainsAny(v, "[]\\|{};@") || strings.Contains(v, "://") {
		return false
	}
	for i := 0; i < len(v); {
		// Excel strings and quoted sheet names escape quotes by doubling them.
		// Only skip function recognition: the external/DDE checks above still
		// inspect every byte, including quoted references.
		if v[i] == '"' || v[i] == '\'' {
			quote := v[i]
			i++
			closed := false
			for i < len(v) {
				if v[i] != quote {
					i++
					continue
				}
				i++
				if i < len(v) && v[i] == quote {
					i++
					continue
				}
				closed = true
				break
			}
			if !closed {
				return false
			}
			continue
		}
		if !formulaNameStart(v[i]) {
			i++
			continue
		}
		start := i
		i++
		for i < len(v) && (formulaNameStart(v[i]) || v[i] >= '0' && v[i] <= '9' || v[i] == '.') {
			i++
		}
		name := v[start:i]
		for i < len(v) && (v[i] == ' ' || v[i] == '\t' || v[i] == '\n' || v[i] == '\r' || v[i] == '\f') {
			i++
		}
		if i < len(v) && v[i] == '(' && !strings.Contains(allowedFormulaFunctions, " "+strings.ToUpper(name)+" ") {
			return false
		}
	}
	return true
}

func formulaNameStart(c byte) bool {
	return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c == '_'
}

func columnNumber(s string) int {
	n := 0
	for _, c := range s {
		n = n*26 + int(c-'A'+1)
	}
	return n
}
func validCell(s string) bool {
	m := cellPattern.FindStringSubmatch(s)
	if m == nil {
		return false
	}
	row, _ := strconv.Atoi(m[2])
	return row <= 1048576 && columnNumber(m[1]) <= 16384
}

// pathType accepts individual nodes only, never query syntax or package paths.
func pathType(format, p string) (string, error) {
	bad := func() (string, error) { return "", fmt.Errorf("%w: unsupported document path", ErrInvalidRequest) }
	if len(p) > MaxPathBytes || !utf8.ValidString(p) || p == "" || p[0] != '/' || strings.ContainsAny(p, "\\\x00\r\n\t") {
		return bad()
	}
	if p == "/" {
		return "document", nil
	}
	parts := strings.Split(p[1:], "/")
	if len(parts) > 12 {
		return bad()
	}
	previous := "document"
	for i, s := range parts {
		if s == "" || s == "." || s == ".." {
			return bad()
		}
		if format == "xlsx" {
			if i == 0 {
				if !checkValue(property{Type: "sheetname"}, s) && !sheetPattern.MatchString(s) {
					return bad()
				}
				previous = "sheet"
				continue
			}
			if i != 1 {
				return bad()
			}
			if validCell(s) {
				previous = "cell"
				continue
			}
			if checkValue(property{Type: "range"}, s) {
				previous = "range"
				continue
			}
			if strings.HasPrefix(s, "col[") && strings.HasSuffix(s, "]") && checkValue(property{Type: "column"}, s[4:len(s)-1]) {
				previous = "col"
				continue
			}
		}
		m := segmentPattern.FindStringSubmatch(s)
		if m == nil {
			return bad()
		}
		typ := m[1]
		switch typ {
		case "p", "para":
			typ = "paragraph"
		case "r":
			typ = "run"
		case "tbl":
			typ = "table"
		case "tr":
			typ = "row"
		case "tc", "td":
			typ = "cell"
		}
		if m[3] != "" {
			if (m[3] == "paraId" && (format != "docx" || typ != "paragraph")) || (m[3] != "paraId" && format != "pptx" && !(format == "xlsx" && typ == "table" && m[3] == "name")) || strings.IndexFunc(m[4], unicode.IsControl) >= 0 {
				return bad()
			}
		}
		e, ok := formats[format][typ]
		if !ok {
			return bad()
		}
		if typ == "body" {
			if format != "docx" || i != 0 || m[2] != "" {
				return bad()
			}
		} else {
			valid := false
			for _, parent := range e.Parents {
				if parent == previous {
					valid = true
				}
			}
			if !valid || m[2] == "" {
				return bad()
			}
		}
		previous = typ
	}
	return previous, nil
}

func validateRequest(req Request) error {
	if _, ok := formats[req.Format]; !ok {
		return fmt.Errorf("%w: unsupported format", ErrInvalidRequest)
	}
	if req.Action == "schema" {
		if len(req.Content) != 0 || len(req.Operations) != 0 || req.Path != "" || req.Depth != 0 {
			return ErrInvalidRequest
		}
		return nil
	}
	if len(req.Content) == 0 || len(req.Content) > MaxFileBytes {
		return fmt.Errorf("%w: file size exceeds limit or is empty", ErrInvalidRequest)
	}
	switch req.Action {
	case "read":
		p := req.Path
		if p == "" {
			p = "/"
		}
		if _, err := pathType(req.Format, p); err != nil {
			return err
		}
		if req.Depth < 0 || req.Depth > MaxDepth || len(req.Operations) != 0 {
			return ErrInvalidRequest
		}
	case "edit":
		if req.Path != "" || req.Depth != 0 || len(req.Operations) == 0 || len(req.Operations) > MaxOperations {
			return ErrInvalidRequest
		}
		encoded, _ := json.Marshal(req.Operations)
		if len(encoded) > MaxOperationsBytes {
			return ErrInvalidRequest
		}
		for _, op := range req.Operations {
			target := op.Path
			if op.Op == "add" {
				target = op.Parent
			}
			typ, err := pathType(req.Format, target)
			if err != nil {
				return err
			}
			if typ == "range" {
				return fmt.Errorf("%w: cell ranges are read-only", ErrInvalidRequest)
			}
			switch op.Op {
			case "add":
				if op.Path != "" || op.Find != "" || op.Replace != "" {
					return ErrInvalidRequest
				}
				e, ok := formats[req.Format][op.Type]
				if !ok || len(e.Parents) == 0 {
					return ErrInvalidRequest
				}
				allowed := false
				for _, p := range e.Parents {
					if p == typ {
						allowed = true
					}
				}
				if !allowed {
					return ErrInvalidRequest
				}
				typ = op.Type
			case "set":
				if op.Parent != "" || op.Type != "" || (len(op.Props) == 0 && op.Find == "") || (op.Replace != "" && op.Find == "") {
					return ErrInvalidRequest
				}
				if op.Find != "" {
					if !checkValue(property{Type: "string"}, op.Find) || !checkValue(property{Type: "string"}, op.Replace) || (req.Format == "xlsx" && (typ != "cell" || len(op.Props) > 0)) || (req.Format != "xlsx" && typ != "document" && typ != "body" && typ != "paragraph" && typ != "run" && typ != "shape" && typ != "cell") {
						return ErrInvalidRequest
					}
				}
			case "remove":
				if op.Parent != "" || op.Type != "" || op.Find != "" || op.Replace != "" || len(op.Props) > 0 || typ == "document" || typ == "body" {
					return ErrInvalidRequest
				}
			default:
				return fmt.Errorf("%w: operation must be set, add or remove", ErrInvalidRequest)
			}
			if len(op.Props) > 32 {
				return ErrInvalidRequest
			}
			for key, v := range op.Props {
				p, ok := formats[req.Format][typ].Properties[key]
				if !ok || (op.Op == "add" && !p.Add) || (op.Op == "set" && !p.Set) || !checkValue(p, v) {
					return fmt.Errorf("%w: unsupported property or value %q for %s", ErrInvalidRequest, key, typ)
				}
				if req.Format == "xlsx" && key == "value" && utf16Length(v) > maxCellUTF16Units {
					return ErrInvalidRequest
				}
				if req.Format == "xlsx" && key == "value" && strings.HasPrefix(v, "=") && !safeFormula(v[1:]) {
					return ErrInvalidRequest
				}
			}
		}
	default:
		return ErrInvalidRequest
	}
	return nil
}
