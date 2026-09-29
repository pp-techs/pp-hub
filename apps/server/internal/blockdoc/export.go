package blockdoc

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"net/url"
	"path"
	"strings"
	"unicode"

	"github.com/nhatbui/pp-hub/apps/server/internal/blob"
	"github.com/nhatbui/pp-hub/apps/server/internal/mdoc"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// Export is a block document ready to be written as a zip: the markdown with
// every attachment it references and holds pointing into assets/, and those
// attachments. Everything that can fail with a status to report fails in
// PrepareExport; WriteZip runs once the response is committed.
type Export struct {
	// Name is the archive's file name, `<document base>.zip`.
	Name string

	files    Files
	doc      store.Node
	mdName   string
	markdown []byte
	assets   []exportAsset
}

type exportAsset struct {
	hash string
	file string // the entry's name under assets/
}

// PrepareExport reads doc's current version and plans its archive. A
// reference to an attachment the document does not hold stays as it is: it
// resolves nowhere, in pp-hub or out of it.
func (s *Service) PrepareExport(ctx context.Context, doc store.Node) (*Export, error) {
	_, ver, rc, err := s.files.OpenFile(ctx, doc.ID)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	// Parsed from the same bytes the lossy path rewrites.
	src, err := readSource(ver, rc)
	if err != nil {
		return nil, err
	}
	parsed, err := mdoc.Parse(src)
	if err != nil {
		return nil, err
	}

	held, err := s.files.ListAttachments(ctx, doc.ID)
	if err != nil {
		return nil, err
	}
	byHash := make(map[string]store.NodeAttachment, len(held))
	for _, a := range held {
		byHash[a.BlobHash] = a
	}
	e := &Export{files: s.files, doc: doc, mdName: markdownName(doc.Name)}
	e.Name = strings.TrimSuffix(e.mdName, path.Ext(e.mdName)) + ".zip"

	links := map[string]string{} // hash → link written into the markdown
	taken := map[string]bool{}
	for _, hash := range mdoc.Attachments(parsed.Doc) {
		att, ok := byHash[hash]
		if !ok || links[hash] != "" {
			continue
		}
		file := uniqueName(assetName(att), taken)
		e.assets = append(e.assets, exportAsset{hash: hash, file: file})
		links[hash] = "assets/" + escapeLink(file)
	}

	switch {
	case len(links) == 0:
		e.markdown = src
	case parsed.Lossy:
		// Rendering a lossy document writes what the block set can hold and
		// drops the rest — an export must not be the thing that loses it. The
		// references are rewritten in the bytes instead; a hash is 64 hex
		// digits, so `attachment:<hash>` cannot be a prefix of anything else.
		out := src
		for hash, link := range links {
			out = bytes.ReplaceAll(out, []byte(mdoc.AttachmentScheme+hash), []byte(link))
		}
		e.markdown = out
	default:
		doc := mdoc.MapAttachments(parsed.Doc, func(hash string) string {
			if link, ok := links[hash]; ok {
				return link
			}
			return mdoc.AttachmentScheme + hash
		})
		e.markdown = mdoc.Render(doc, parsed.Frontmatter)
	}
	return e, nil
}

// WriteZip streams the archive to w: the markdown first, then each asset
// copied straight from the blob store. An asset that fails to open ends the
// archive with the error, which the caller can only log — the response is
// already under way.
func (e *Export) WriteZip(ctx context.Context, w io.Writer) error {
	zw := zip.NewWriter(w)
	entry, err := zw.CreateHeader(&zip.FileHeader{Name: e.mdName, Method: zip.Deflate})
	if err != nil {
		return err
	}
	if _, err := entry.Write(e.markdown); err != nil {
		return err
	}
	for _, a := range e.assets {
		if err := e.writeAsset(ctx, zw, a); err != nil {
			return err
		}
	}
	return zw.Close()
}

func (e *Export) writeAsset(ctx context.Context, zw *zip.Writer, a exportAsset) error {
	att, rc, err := e.files.OpenAttachment(ctx, e.doc.ID, a.hash)
	if err != nil {
		return fmt.Errorf("attachment %s: %w", a.hash[:12], err)
	}
	defer rc.Close()
	// Images and videos are compressed already; deflating them again costs
	// CPU for nothing.
	method := zip.Deflate
	if t := strings.ToLower(att.Mime); strings.HasPrefix(t, "image/") || strings.HasPrefix(t, "video/") {
		method = zip.Store
	}
	entry, err := zw.CreateHeader(&zip.FileHeader{Name: "assets/" + a.file, Method: method, Modified: att.CreatedAt.Time})
	if err != nil {
		return err
	}
	_, err = io.Copy(entry, rc)
	return err
}

// markdownName is the entry the document is written as: its own name, with
// .md added if it lacks it.
func markdownName(name string) string {
	name = cleanName(name)
	if name == "" {
		name = "document"
	}
	if !strings.EqualFold(path.Ext(name), ".md") {
		name += ".md"
	}
	return name
}

// assetName is an attachment's file name made safe for a zip entry: the base
// of whatever path it carried, without control characters, falling back to
// the hash (and an extension for its type) when nothing is left.
func assetName(att store.NodeAttachment) string {
	if name := cleanName(att.Name); name != "" {
		return name
	}
	name := att.BlobHash
	if blob.IsHash(name) {
		name = name[:12]
	}
	if exts, _ := mime.ExtensionsByType(att.Mime); len(exts) > 0 {
		name += exts[0]
	}
	return name
}

func cleanName(name string) string {
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "." || name == ".." {
		return ""
	}
	return name
}

// uniqueName is name, or name with "-2", "-3"… before its extension, whichever
// is not taken yet (case-insensitively: archives are unpacked onto
// case-insensitive filesystems). It marks the result taken.
func uniqueName(name string, taken map[string]bool) string {
	ext := path.Ext(name)
	base := strings.TrimSuffix(name, ext)
	if base == "" {
		base, ext = name, ""
	}
	candidate := name
	for n := 2; taken[strings.ToLower(candidate)]; n++ {
		candidate = fmt.Sprintf("%s-%d%s", base, n, ext)
	}
	taken[strings.ToLower(candidate)] = true
	return candidate
}

// escapeLink makes a file name safe inside a markdown link destination:
// percent-encoded like a URL path segment, parentheses included, so a space or
// an unbalanced paren cannot end the link early.
func escapeLink(name string) string {
	s := url.PathEscape(name)
	return strings.NewReplacer("(", "%28", ")", "%29").Replace(s)
}
