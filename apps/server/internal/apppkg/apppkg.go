// Package apppkg reads and validates uploaded packaged-app bundles (.zip with
// a pp-app.json manifest declaring web.entry).
package apppkg

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// Limits for uploads and manifests.
const (
	MaxUploadBytes = 20 << 20
	MaxFiles       = 2000
	MaxFileBytes   = 10 << 20
	MaxTotalBytes  = 50 << 20
	MaxRecordBytes = 64 << 10
	SDKVersion     = 1

	maxIconBytes   = 256 << 10
	maxPathBytes   = 512
	maxKinds       = 8
	maxSeeds       = 100
	maxConnect     = 10
	maxTemplate    = 200
	maxLocales     = 50
	maxStrings     = 500
	maxStringChars = 500
	manifestName   = "pp-app.json"
)

// CollectionPattern matches record collection names (also app ids and kind suffixes).
const CollectionPattern = `^[a-z][a-z0-9_]{0,31}$`

// VerbPattern matches notification verbs.
const VerbPattern = `^[a-z][a-z0-9_.-]{0,31}$`

var (
	collectionRe = regexp.MustCompile(CollectionPattern)
	verbRe       = regexp.MustCompile(VerbPattern)
	localeRe     = regexp.MustCompile(`^[a-z]{2,3}(-[A-Za-z0-9]{2,8})?$`)
	semverRe     = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`)
)

// ValidCollection reports whether s is a valid record collection name.
func ValidCollection(s string) bool { return collectionRe.MatchString(s) }

// ValidVerb reports whether s is a valid notification verb.
func ValidVerb(s string) bool { return verbRe.MatchString(s) }

// Manifest is a validated packaged-app manifest.
type Manifest struct {
	ID, Name, Version, Description, Author, License, Homepage string
	SDK                                                       int
	Icon                                                      string
	NodeKinds                                                 []NodeKind
	Web                                                       Web
	Backend                                                   *Backend // nil when absent
	Notifications                                             map[string]string
	Translations                                              map[string]map[string]string
}

// NodeKind is a node kind the app contributes. NewLabel and DefaultName are
// filled with defaults when absent.
type NodeKind struct {
	Kind, Label, NewLabel, DefaultName, NewHint, Icon string
	Seed                                              []Seed
}

// Seed is a record created with every new node of a kind.
type Seed struct {
	Collection string
	Data       json.RawMessage
}

// Web describes the app's UI.
type Web struct {
	Entry   string
	Connect []string
}

// Backend is the author's own service.
type Backend struct {
	URL string
}

// File is one file of a bundle.
type File struct {
	Path, ContentType string
	Body              []byte
	SHA256            string // hex
}

// Bundle is a validated package.
type Bundle struct {
	Manifest     Manifest
	ManifestJSON []byte
	Files        []File // sorted by Path
	Digest       string
	Size         int64 // total uncompressed
}

// Error is a problem with the upload, worded for the admin who uploaded it.
type Error struct{ Msg string }

func (e *Error) Error() string { return e.Msg }

func errf(format string, args ...any) error { return &Error{Msg: fmt.Sprintf(format, args...)} }

type rawManifest struct {
	ID            string                       `json:"id"`
	Name          string                       `json:"name"`
	Version       string                       `json:"version"`
	Description   string                       `json:"description"`
	Author        string                       `json:"author"`
	License       string                       `json:"license"`
	Homepage      string                       `json:"homepage"`
	Engines       *rawEngines                  `json:"engines"`
	Icon          string                       `json:"icon"`
	NodeKinds     []rawKind                    `json:"nodeKinds"`
	Web           *rawWeb                      `json:"web"`
	Backend       *rawBackend                  `json:"backend"`
	Notifications map[string]string            `json:"notifications"`
	Translations  map[string]map[string]string `json:"translations"`
}

type rawEngines struct {
	SDK *int `json:"sdk"`
}

type rawKind struct {
	Kind        string    `json:"kind"`
	Label       string    `json:"label"`
	NewLabel    string    `json:"newLabel"`
	DefaultName string    `json:"defaultName"`
	NewHint     string    `json:"newHint"`
	Icon        string    `json:"icon"`
	Seed        []rawSeed `json:"seed"`
}

type rawSeed struct {
	Collection string          `json:"collection"`
	Data       json.RawMessage `json:"data"`
}

type rawWeb struct {
	Entry   string   `json:"entry"`
	Connect []string `json:"connect"`
}

type rawBackend struct {
	URL string `json:"url"`
}

func checkText(field, v string, required bool, max int) error {
	if required && strings.TrimSpace(v) == "" {
		return errf("pp-app.json: %s is required", field)
	}
	if utf8.RuneCountInString(v) > max {
		return errf("pp-app.json: %s must be at most %d characters", field, max)
	}
	return nil
}

func checkHTTPURL(field, v string) (*url.URL, error) {
	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, errf("pp-app.json: %s %q must be an http(s) URL", field, v)
	}
	return u, nil
}

// ParseManifest strictly decodes and validates a packaged-app manifest.
func ParseManifest(data []byte) (Manifest, error) {
	var r rawManifest
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r); err != nil {
		return Manifest{}, errf("pp-app.json: %v", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Manifest{}, errf("pp-app.json: unexpected data after the JSON object")
	}

	if !collectionRe.MatchString(r.ID) {
		return Manifest{}, errf("pp-app.json: id %q must match %s", r.ID, CollectionPattern)
	}
	if err := checkText("name", r.Name, true, 60); err != nil {
		return Manifest{}, err
	}
	if !semverRe.MatchString(r.Version) {
		return Manifest{}, errf("pp-app.json: version %q must be a semantic version like 1.0.0", r.Version)
	}
	if err := checkText("description", r.Description, true, 300); err != nil {
		return Manifest{}, err
	}
	if err := checkText("author", r.Author, true, 100); err != nil {
		return Manifest{}, err
	}
	if r.Homepage != "" {
		if _, err := checkHTTPURL("homepage", r.Homepage); err != nil {
			return Manifest{}, err
		}
	}
	if r.Engines == nil || r.Engines.SDK == nil {
		return Manifest{}, errf("pp-app.json: engines.sdk is required (this server supports sdk %d)", SDKVersion)
	}
	if *r.Engines.SDK != SDKVersion {
		return Manifest{}, errf("pp-app.json: engines.sdk %d is not supported; this server supports sdk %d", *r.Engines.SDK, SDKVersion)
	}
	if r.Icon != "" {
		if err := checkIconName("icon", r.Icon); err != nil {
			return Manifest{}, err
		}
	}

	m := Manifest{
		ID: r.ID, Name: r.Name, Version: r.Version, Description: r.Description,
		Author: r.Author, License: r.License, Homepage: r.Homepage,
		SDK: *r.Engines.SDK, Icon: r.Icon,
	}

	if len(r.NodeKinds) < 1 || len(r.NodeKinds) > maxKinds {
		return Manifest{}, errf("pp-app.json: nodeKinds must have 1 to %d entries", maxKinds)
	}
	seen := map[string]bool{}
	for i, k := range r.NodeKinds {
		f := fmt.Sprintf("nodeKinds[%d]", i)
		prefix := r.ID + "."
		if !strings.HasPrefix(k.Kind, prefix) {
			return Manifest{}, errf("pp-app.json: %s.kind %q must start with %q", f, k.Kind, prefix)
		}
		if !collectionRe.MatchString(strings.TrimPrefix(k.Kind, prefix)) {
			return Manifest{}, errf("pp-app.json: %s.kind %q: the part after %q must match %s", f, k.Kind, prefix, CollectionPattern)
		}
		if seen[k.Kind] {
			return Manifest{}, errf("pp-app.json: %s.kind %q is declared twice", f, k.Kind)
		}
		seen[k.Kind] = true
		if err := checkText(f+".label", k.Label, true, 60); err != nil {
			return Manifest{}, err
		}
		if err := checkText(f+".newLabel", k.NewLabel, false, 60); err != nil {
			return Manifest{}, err
		}
		if err := checkText(f+".defaultName", k.DefaultName, false, 120); err != nil {
			return Manifest{}, err
		}
		if err := checkText(f+".newHint", k.NewHint, false, 200); err != nil {
			return Manifest{}, err
		}
		if k.Icon != "" {
			if err := checkIconName(f+".icon", k.Icon); err != nil {
				return Manifest{}, err
			}
		}
		if len(k.Seed) > maxSeeds {
			return Manifest{}, errf("pp-app.json: %s.seed must have at most %d entries", f, maxSeeds)
		}
		nk := NodeKind{Kind: k.Kind, Label: k.Label, NewLabel: k.NewLabel, DefaultName: k.DefaultName, NewHint: k.NewHint, Icon: k.Icon}
		if strings.TrimSpace(nk.NewLabel) == "" {
			nk.NewLabel = "New " + k.Label
		}
		if strings.TrimSpace(nk.DefaultName) == "" {
			nk.DefaultName = k.Label
		}
		for j, s := range k.Seed {
			sf := fmt.Sprintf("%s.seed[%d]", f, j)
			if !collectionRe.MatchString(s.Collection) {
				return Manifest{}, errf("pp-app.json: %s.collection %q must match %s", sf, s.Collection, CollectionPattern)
			}
			var obj map[string]json.RawMessage
			trimmed := bytes.TrimSpace(s.Data)
			if len(trimmed) == 0 || trimmed[0] != '{' || json.Unmarshal(trimmed, &obj) != nil {
				return Manifest{}, errf("pp-app.json: %s.data must be a JSON object", sf)
			}
			var buf bytes.Buffer
			if err := json.Compact(&buf, trimmed); err != nil {
				return Manifest{}, errf("pp-app.json: %s.data must be a JSON object", sf)
			}
			if buf.Len() > MaxRecordBytes {
				return Manifest{}, errf("pp-app.json: %s.data must be at most %d bytes", sf, MaxRecordBytes)
			}
			nk.Seed = append(nk.Seed, Seed{Collection: s.Collection, Data: json.RawMessage(buf.Bytes())})
		}
		m.NodeKinds = append(m.NodeKinds, nk)
	}

	if r.Web == nil || r.Web.Entry == "" {
		return Manifest{}, errf("pp-app.json: web.entry is required")
	}
	if !strings.HasSuffix(r.Web.Entry, ".html") {
		return Manifest{}, errf("pp-app.json: web.entry %q must be an .html file", r.Web.Entry)
	}
	if len(r.Web.Connect) > maxConnect {
		return Manifest{}, errf("pp-app.json: web.connect must have at most %d entries", maxConnect)
	}
	seenOrigin := map[string]bool{}
	for i, c := range r.Web.Connect {
		if seenOrigin[c] {
			return Manifest{}, errf("pp-app.json: web.connect[%d] %q is listed twice", i, c)
		}
		seenOrigin[c] = true
		u, err := url.Parse(c)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" ||
			u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || strings.ContainsAny(c, "?#") ||
			"https://"+u.Host != c {
			return Manifest{}, errf("pp-app.json: web.connect[%d] %q must be a bare https origin like https://api.example.com", i, c)
		}
	}
	m.Web = Web{Entry: r.Web.Entry, Connect: r.Web.Connect}

	if r.Backend != nil {
		u, err := checkHTTPURL("backend.url", r.Backend.URL)
		if err != nil {
			return Manifest{}, err
		}
		if u.User != nil {
			return Manifest{}, errf("pp-app.json: backend.url %q must not contain user info", r.Backend.URL)
		}
		if u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(r.Backend.URL, "?#") {
			return Manifest{}, errf("pp-app.json: backend.url %q must not have a query or fragment", r.Backend.URL)
		}
		m.Backend = &Backend{URL: r.Backend.URL}
	}

	for verb, tpl := range r.Notifications {
		if !verbRe.MatchString(verb) {
			return Manifest{}, errf("pp-app.json: notifications verb %q must match %s", verb, VerbPattern)
		}
		if strings.TrimSpace(tpl) == "" || utf8.RuneCountInString(tpl) > maxTemplate {
			return Manifest{}, errf("pp-app.json: notifications.%s template must be 1 to %d characters", verb, maxTemplate)
		}
	}
	m.Notifications = r.Notifications

	if len(r.Translations) > maxLocales {
		return Manifest{}, errf("pp-app.json: translations must have at most %d locales", maxLocales)
	}
	for loc, strs := range r.Translations {
		if !localeRe.MatchString(loc) {
			return Manifest{}, errf("pp-app.json: translations locale %q is not a valid locale like \"vi\" or \"pt-BR\"", loc)
		}
		if len(strs) > maxStrings {
			return Manifest{}, errf("pp-app.json: translations.%s must have at most %d strings", loc, maxStrings)
		}
		for k, v := range strs {
			if utf8.RuneCountInString(k) > maxStringChars || utf8.RuneCountInString(v) > maxStringChars {
				return Manifest{}, errf("pp-app.json: translations.%s: strings must be at most %d characters", loc, maxStringChars)
			}
		}
	}
	m.Translations = r.Translations
	return m, nil
}

func checkIconName(field, p string) error {
	ext := path.Ext(p)
	if ext != ".svg" && ext != ".png" {
		return errf("pp-app.json: %s %q must be an .svg or .png file", field, p)
	}
	return nil
}

func checkPath(name string) error {
	if len(name) > maxPathBytes {
		return errf("package: path %q is longer than %d bytes", name, maxPathBytes)
	}
	if strings.ContainsRune(name, 0) {
		return errf("package: path %q contains a NUL byte", name)
	}
	if strings.Contains(name, `\`) {
		return errf("package: path %q contains a backslash", name)
	}
	if strings.HasPrefix(name, "/") {
		return errf("package: path %q is absolute", name)
	}
	for _, seg := range strings.Split(strings.TrimSuffix(name, "/"), "/") {
		if seg == "" || seg == "." || seg == ".." {
			return errf("package: path %q has an empty, \".\" or \"..\" segment", name)
		}
	}
	return nil
}

func skipped(name string) bool {
	for _, seg := range strings.Split(name, "/") {
		if seg == "__MACOSX" || strings.HasPrefix(seg, ".") {
			return true
		}
	}
	return false
}

// Read parses and validates an uploaded package.
func Read(zipBytes []byte) (Bundle, error) {
	if len(zipBytes) > MaxUploadBytes {
		return Bundle{}, errf("package: upload is larger than %d MiB", MaxUploadBytes>>20)
	}
	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return Bundle{}, errf("package: not a valid zip file: %v", err)
	}

	type entry struct {
		name string
		f    *zip.File
	}
	var kept []entry
	for _, f := range zr.File {
		if err := checkPath(f.Name); err != nil {
			return Bundle{}, err
		}
		if strings.HasSuffix(f.Name, "/") || skipped(f.Name) {
			continue
		}
		mode := f.Mode()
		if mode&0o170000 == 0o120000 || mode.Type()&(1<<27) != 0 { // symlink
			return Bundle{}, errf("package: %q is a symlink; symlinks are not allowed", f.Name)
		}
		if f.FileInfo().IsDir() {
			continue
		}
		if !mode.IsRegular() {
			return Bundle{}, errf("package: %q is not a regular file", f.Name)
		}
		kept = append(kept, entry{f.Name, f})
		if len(kept) > MaxFiles {
			return Bundle{}, errf("package: more than %d files", MaxFiles)
		}
	}

	// Strip a single shared top-level directory that holds pp-app.json.
	hasRoot := false
	for _, e := range kept {
		if e.name == manifestName {
			hasRoot = true
			break
		}
	}
	if !hasRoot && len(kept) > 0 {
		first, _, ok := strings.Cut(kept[0].name, "/")
		shared := ok
		holds := false
		for _, e := range kept {
			seg, rest, ok := strings.Cut(e.name, "/")
			if !ok || seg != first {
				shared = false
				break
			}
			if rest == manifestName {
				holds = true
			}
		}
		if shared && holds {
			for i := range kept {
				kept[i].name = strings.TrimPrefix(kept[i].name, first+"/")
			}
		}
	}

	files := make([]File, 0, len(kept))
	seen := map[string]bool{}
	var total int64
	for _, e := range kept {
		if seen[e.name] {
			return Bundle{}, errf("package: duplicate path %q", e.name)
		}
		seen[e.name] = true
		rc, err := e.f.Open()
		if err != nil {
			return Bundle{}, errf("package: cannot read %q: %v", e.name, err)
		}
		body, err := io.ReadAll(io.LimitReader(rc, MaxFileBytes+1))
		rc.Close()
		if err != nil && len(body) <= MaxFileBytes {
			return Bundle{}, errf("package: cannot read %q: %v", e.name, err)
		}
		if len(body) > MaxFileBytes {
			return Bundle{}, errf("package: %q is larger than %d MiB", e.name, MaxFileBytes>>20)
		}
		total += int64(len(body))
		if total > MaxTotalBytes {
			return Bundle{}, errf("package: total uncompressed size is larger than %d MiB", MaxTotalBytes>>20)
		}
		sum := sha256.Sum256(body)
		files = append(files, File{Path: e.name, ContentType: ContentType(e.name), Body: body, SHA256: hex.EncodeToString(sum[:])})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })

	byPath := make(map[string]*File, len(files))
	for i := range files {
		byPath[files[i].Path] = &files[i]
	}
	mf, ok := byPath[manifestName]
	if !ok {
		return Bundle{}, errf("package: pp-app.json not found at the zip root or in a single top-level folder")
	}
	m, err := ParseManifest(mf.Body)
	if err != nil {
		return Bundle{}, err
	}
	if _, ok := byPath[m.Web.Entry]; !ok {
		return Bundle{}, errf("pp-app.json: web.entry %q is not in the package", m.Web.Entry)
	}
	checkIcon := func(field, p string) error {
		if p == "" {
			return nil
		}
		f, ok := byPath[p]
		if !ok {
			return errf("pp-app.json: %s %q is not in the package", field, p)
		}
		if len(f.Body) > maxIconBytes {
			return errf("pp-app.json: %s %q is larger than 256 KiB", field, p)
		}
		return nil
	}
	if err := checkIcon("icon", m.Icon); err != nil {
		return Bundle{}, err
	}
	for i, k := range m.NodeKinds {
		if err := checkIcon(fmt.Sprintf("nodeKinds[%d].icon", i), k.Icon); err != nil {
			return Bundle{}, err
		}
	}

	h := sha256.New()
	for _, f := range files {
		io.WriteString(h, f.Path+"\x00"+f.SHA256+"\n")
	}
	return Bundle{
		Manifest:     m,
		ManifestJSON: mf.Body,
		Files:        files,
		Digest:       hex.EncodeToString(h.Sum(nil)),
		Size:         total,
	}, nil
}

var contentTypes = map[string]string{
	".html":  "text/html; charset=utf-8",
	".js":    "text/javascript; charset=utf-8",
	".mjs":   "text/javascript; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".json":  "application/json",
	".svg":   "image/svg+xml",
	".png":   "image/png",
	".jpg":   "image/jpeg",
	".jpeg":  "image/jpeg",
	".gif":   "image/gif",
	".webp":  "image/webp",
	".ico":   "image/x-icon",
	".woff":  "font/woff",
	".woff2": "font/woff2",
	".ttf":   "font/ttf",
	".wasm":  "application/wasm",
	".map":   "application/json",
	".txt":   "text/plain; charset=utf-8",
	".md":    "text/markdown; charset=utf-8",
}

// ContentType returns the Content-Type served for a package path.
func ContentType(p string) string {
	ext := strings.ToLower(path.Ext(p))
	if ct, ok := contentTypes[ext]; ok {
		return ct
	}
	return "application/octet-stream"
}
