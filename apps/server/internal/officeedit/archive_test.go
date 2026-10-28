package officeedit

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"testing"
)

type zipEntry struct {
	name, data string
	flags      uint16
	mode       fs.FileMode
}

func archiveBytes(t *testing.T, entries []zipEntry) []byte {
	t.Helper()
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Deflate, Flags: entry.flags}
		if entry.mode != 0 {
			header.SetMode(entry.mode)
		}
		w, err := zw.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(entry.data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
func packageEntries(format string) []zipEntry {
	main := mainParts[format]
	return []zipEntry{
		{name: "[Content_Types].xml", data: fmt.Sprintf(`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Override PartName="/%s" ContentType="%s"/></Types>`, main.Path, main.MIME)},
		{name: "_rels/.rels", data: fmt.Sprintf(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="%s"/></Relationships>`, main.Path)},
		{name: main.Path, data: `<document/>`},
	}
}

func TestPackageRejectsHostileArchivesBeforeCLI(t *testing.T) {
	for _, format := range []string{"docx", "xlsx", "pptx"} {
		if err := validatePackage(archiveBytes(t, packageEntries(format)), format); err != nil {
			t.Fatalf("valid %s container rejected: %v", format, err)
		}
	}
	cases := map[string]func([]zipEntry) []zipEntry{
		"traversal":      func(e []zipEntry) []zipEntry { return append(e, zipEntry{name: "../outside.xml", data: "x"}) },
		"absolute":       func(e []zipEntry) []zipEntry { return append(e, zipEntry{name: "/tmp/outside", data: "x"}) },
		"backslash":      func(e []zipEntry) []zipEntry { return append(e, zipEntry{name: `word\evil.xml`, data: "x"}) },
		"duplicate":      func(e []zipEntry) []zipEntry { return append(e, e[2]) },
		"case_duplicate": func(e []zipEntry) []zipEntry { x := e[2]; x.name = strings.ToUpper(x.name); return append(e, x) },
		"symlink": func(e []zipEntry) []zipEntry {
			return append(e, zipEntry{name: "word/link", data: "/etc/passwd", mode: fs.ModeSymlink | 0777})
		},
		"macro": func(e []zipEntry) []zipEntry { return append(e, zipEntry{name: "word/vbaProject.bin", data: "macro"}) },
		"embedding": func(e []zipEntry) []zipEntry {
			return append(e, zipEntry{name: "word/embeddings/oleObject1.bin", data: "ole"})
		},
		"macro_mime": func(e []zipEntry) []zipEntry {
			e[0].data = strings.ReplaceAll(e[0].data, "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml", "application/vnd.ms-word.document.macroEnabled.main+xml")
			return e
		},
		"doctype": func(e []zipEntry) []zipEntry {
			e[2].data = `<!DOCTYPE x [<!ENTITY x SYSTEM "file:///etc/passwd">]><document>&x;</document>`
			return e
		},
		"external_media": func(e []zipEntry) []zipEntry {
			return append(e, zipEntry{name: "word/_rels/document.xml.rels", data: `<Relationships><Relationship Id="x" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="https://example.com/a.png" TargetMode="External"/></Relationships>`})
		},
		"local_hyperlink": func(e []zipEntry) []zipEntry {
			return append(e, zipEntry{name: "word/_rels/document.xml.rels", data: `<Relationships><Relationship Id="x" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="file:///etc/passwd" TargetMode="External"/></Relationships>`})
		},
		"relationship_escape": func(e []zipEntry) []zipEntry {
			return append(e, zipEntry{name: "word/_rels/document.xml.rels", data: `<Relationships><Relationship Id="x" Type="image" Target="../../etc/passwd"/></Relationships>`})
		},
		"bomb": func(e []zipEntry) []zipEntry {
			return append(e, zipEntry{name: "word/bomb.xml", data: strings.Repeat("x", (1<<20)+1)})
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			if err := validatePackage(archiveBytes(t, mutate(packageEntries("docx"))), "docx"); !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("unsafe archive accepted: %v", err)
			}
		})
	}
	for _, data := range [][]byte{[]byte("not ZIP"), []byte("\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1")} {
		if !errors.Is(validatePackage(data, "docx"), ErrInvalidRequest) {
			t.Fatal("legacy or non-ZIP input accepted")
		}
	}
	if !errors.Is(validatePackage(archiveBytes(t, packageEntries("docx")), "xlsx"), ErrInvalidRequest) {
		t.Fatal("extension/content mismatch accepted")
	}
}

func TestPackageAllowsOnlyOrdinaryExternalHyperlinks(t *testing.T) {
	for _, target := range []string{"https://example.com", "http://example.com", "mailto:reader@example.com"} {
		entries := append(packageEntries("docx"), zipEntry{name: "word/_rels/document.xml.rels", data: fmt.Sprintf(`<Relationships><Relationship Id="x" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="%s" TargetMode="External"/></Relationships>`, target)})
		if err := validatePackage(archiveBytes(t, entries), "docx"); err != nil {
			t.Fatalf("ordinary hyperlink rejected: %v", err)
		}
	}
}

func TestPackageRejectsEncryptedFlagAndBadCRC(t *testing.T) {
	data := archiveBytes(t, packageEntries("docx"))
	// Set the encryption bit in both central and local headers; ZIP still parses.
	encrypted := bytes.Clone(data)
	for start := 0; start < len(encrypted)-10; start++ {
		if bytes.Equal(encrypted[start:start+4], []byte{'P', 'K', 3, 4}) {
			encrypted[start+6] |= 1
		}
		if bytes.Equal(encrypted[start:start+4], []byte{'P', 'K', 1, 2}) {
			encrypted[start+8] |= 1
		}
	}
	if !errors.Is(validatePackage(encrypted, "docx"), ErrInvalidRequest) {
		t.Fatal("encrypted archive accepted")
	}
	corrupt := bytes.Clone(data)
	// Corrupt the first compressed stream without changing directory sizes.
	zr, err := zip.NewReader(bytes.NewReader(corrupt), int64(len(corrupt)))
	if err != nil {
		t.Fatal(err)
	}
	offset, err := zr.File[0].DataOffset()
	if err != nil {
		t.Fatal(err)
	}
	corrupt[offset] ^= 0xff
	if !errors.Is(validatePackage(corrupt, "docx"), ErrInvalidRequest) {
		t.Fatal("corrupt stream/checksum accepted")
	}
}
