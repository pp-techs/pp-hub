package officeedit

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"
)

const (
	maxEntries       = 2048
	maxExpandedBytes = 64 << 20
	maxEntryBytes    = 16 << 20
)

var mainParts = map[string]struct{ Path, MIME string }{
	"docx": {"word/document.xml", "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"},
	"xlsx": {"xl/workbook.xml", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"},
	"pptx": {"ppt/presentation.xml", "application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml"},
}

func validatePackage(content []byte, format string) error {
	invalid := func(reason string) error {
		return fmt.Errorf("%w: unsafe or invalid Office package (%s)", ErrInvalidRequest, reason)
	}
	if len(content) == 0 || len(content) > MaxFileBytes {
		return invalid("size")
	}
	zr, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil || len(zr.File) == 0 || len(zr.File) > maxEntries {
		return invalid("ZIP directory")
	}
	entries := make(map[string]bool, len(zr.File))
	var expanded uint64
	for _, f := range zr.File {
		name := f.Name
		lower := strings.ToLower(name)
		if len(name) > 512 || strings.ContainsAny(name, "\\\x00:%?#") || strings.HasPrefix(name, "/") || path.Clean(name) != strings.TrimSuffix(name, "/") || name == "." || strings.HasPrefix(name, "../") || f.Mode().Type() != 0 && !f.FileInfo().IsDir() {
			return invalid("entry path or mode")
		}
		if entries[lower] || f.Flags&0x41 != 0 || (f.Method != zip.Store && f.Method != zip.Deflate) {
			return invalid("duplicate, encrypted or unsupported entry")
		}
		entries[lower] = true
		for _, blocked := range []string{"vbaproject", "vbasignature", "macrosheets/", "dialogsheets/", "activex/", "embeddings/", "customui/", "externallinks/", "connections.xml", "querytables/", "webextensions/", "oleobject", "attachedtemplate"} {
			if strings.Contains(lower, blocked) {
				return invalid("active or external content")
			}
		}
		if strings.HasSuffix(lower, ".bin") || strings.HasSuffix(lower, ".exe") || strings.HasSuffix(lower, ".dll") {
			return invalid("executable content")
		}
		expanded += f.UncompressedSize64
		if expanded > maxExpandedBytes || f.UncompressedSize64 > maxEntryBytes || f.UncompressedSize64 > 1<<20 && (f.CompressedSize64 == 0 || f.UncompressedSize64/f.CompressedSize64 > 200) {
			return invalid("expansion limit")
		}
	}
	main, ok := mainParts[format]
	if !ok || !entries[main.Path] || !entries["[content_types].xml"] || !entries["_rels/.rels"] {
		return invalid("package type")
	}
	var actualTotal int64
	var mainType, defaultMainType string
	var mainOverride, rootRelation bool
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		r, err := f.Open()
		if err != nil {
			return invalid("entry open")
		}
		data, readErr := io.ReadAll(io.LimitReader(r, maxEntryBytes+1))
		closeErr := r.Close()
		actualTotal += int64(len(data))
		if readErr != nil || closeErr != nil || len(data) > maxEntryBytes || actualTotal > maxExpandedBytes || uint64(len(data)) != f.UncompressedSize64 {
			return invalid("entry bytes or checksum")
		}
		lower := strings.ToLower(f.Name)
		if !strings.HasSuffix(lower, ".xml") && !strings.HasSuffix(lower, ".rels") {
			continue
		}
		dec := xml.NewDecoder(bytes.NewReader(data))
		depth := 0
		for {
			token, e := dec.Token()
			if e == io.EOF {
				break
			}
			if e != nil {
				return invalid("XML")
			}
			switch t := token.(type) {
			case xml.Directive:
				return invalid("XML directive")
			case xml.StartElement:
				depth++
				if depth > 128 {
					return invalid("XML depth")
				}
				attrs := make(map[string]string, len(t.Attr))
				for _, a := range t.Attr {
					attrs[a.Name.Local] = a.Value
				}
				if lower == "[content_types].xml" && (t.Name.Local == "Default" || t.Name.Local == "Override") {
					ct := strings.ToLower(attrs["ContentType"])
					if strings.Contains(ct, "macro") || strings.Contains(ct, "vba") || strings.Contains(ct, "oleobject") || strings.Contains(ct, "activex") {
						return invalid("active content type")
					}
					if t.Name.Local == "Default" && attrs["Extension"] == strings.TrimPrefix(path.Ext(main.Path), ".") {
						defaultMainType = attrs["ContentType"]
					}
					if t.Name.Local == "Override" && attrs["PartName"] == "/"+main.Path {
						mainOverride = true
						mainType = attrs["ContentType"]
					}
				}
				if strings.HasSuffix(lower, ".rels") && t.Name.Local == "Relationship" {
					target := attrs["Target"]
					u, e := url.Parse(target)
					if strings.EqualFold(attrs["TargetMode"], "External") && strings.HasSuffix(attrs["Type"], "/hyperlink") && e == nil && (u.Scheme == "http" || u.Scheme == "https" || u.Scheme == "mailto") {
						continue
					}
					if e != nil || attrs["TargetMode"] != "" && !strings.EqualFold(attrs["TargetMode"], "Internal") || u.IsAbs() || u.Host != "" || strings.ContainsAny(target, "\\\x00") {
						return invalid("external relationship")
					}
					decoded, e := url.PathUnescape(u.Path)
					if e != nil {
						return invalid("relationship URI")
					}
					// _rels/foo.xml.rels is relative to foo.xml's directory, not _rels.
					base := path.Dir(path.Dir(f.Name))
					resolved := path.Clean(path.Join(base, decoded))
					if strings.HasPrefix(decoded, "/") {
						resolved = path.Clean(strings.TrimPrefix(decoded, "/"))
					}
					// Internal relationships cannot escape the package. Optional
					// dangling parts are left to the OpenXML parser; editor-produced
					// documents can retain them after removing the corresponding part.
					if strings.HasPrefix(resolved, "../") || resolved == ".." {
						return invalid("relationship target")
					}
					if lower == "_rels/.rels" && strings.HasSuffix(attrs["Type"], "/officeDocument") && resolved == main.Path {
						rootRelation = true
					}
				}
			case xml.EndElement:
				depth--
			}
		}
	}
	if !mainOverride {
		mainType = defaultMainType
	}
	if mainType != main.MIME || !rootRelation {
		return invalid("main document relationship or content type")
	}
	return nil
}
