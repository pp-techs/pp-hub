package httpapi

import (
	"bytes"
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

func init() {
	// Go's built-in table has no fonts, manifests or icons, and the alpine
	// image carries no /etc/mime.types to fall back on. A missing type would
	// ship them as application/octet-stream, which `nosniff` then refuses to
	// treat as a font or stylesheet.
	for ext, typ := range map[string]string{
		".woff":        "font/woff",
		".woff2":       "font/woff2",
		".webmanifest": "application/manifest+json",
		".ico":         "image/x-icon",
	} {
		_ = mime.AddExtensionType(ext, typ)
	}
}

// WebApp serves the built single-page app on the API's own origin.
//
// The session cookie is host-only with SameSite=Lax and the API client sends
// credentials "same-origin", so the bundle and the API have to share a
// hostname. This is what used to be a separate nginx container; having the
// Go server do it removes a hop, a second image and a second place where
// the route table had to be kept in sync with this one.
type WebApp struct {
	root      fs.FS
	index     []byte
	indexTime time.Time
	// frameSrc is the Document Server's origin, named in the CSP because the
	// editor handoff is a real cross-origin form POST into an iframe.
	frameSrc string
	// hsts is whether Strict-Transport-Security is sent: only when the
	// public address is https, since the header means nothing over http.
	hsts     bool
	compress func(http.Handler) http.Handler
}

// NewWebApp opens the bundle in dir. It fails when dir has no index.html, so
// a wrong PP_WEB_DIR is a startup error rather than a site that answers 404
// to everything.
func NewWebApp(dir, onlyOfficeURL, externalURL string) (*WebApp, error) {
	root := os.DirFS(dir)
	info, err := fs.Stat(root, "index.html")
	if err != nil {
		return nil, fmt.Errorf("PP_WEB_DIR %q: %w", dir, err)
	}
	index, err := fs.ReadFile(root, "index.html")
	if err != nil {
		return nil, fmt.Errorf("PP_WEB_DIR %q: %w", dir, err)
	}
	return &WebApp{
		root:      root,
		index:     index,
		indexTime: info.ModTime(),
		frameSrc:  strings.TrimSpace(onlyOfficeURL),
		hsts:      strings.HasPrefix(externalURL, "https://"),
		// Vite emits content-hashed names, so compressing on the fly costs
		// one pass per cold cache entry and nothing after.
		compress: middleware.Compress(6,
			"application/javascript",
			"text/javascript",
			"text/css",
			"text/html",
			"application/json",
			"image/svg+xml",
			"application/manifest+json",
			"text/plain",
		),
	}, nil
}

// apiPrefixes are the path trees the API owns. A request under one of them
// that matched no route is a 404, never the app shell: a mistyped API call
// answering 200 with HTML is the failure that is hardest to see.
var apiPrefixes = []string{"/api", "/auth", "/hooks", "/mcp", "/dav", "/wopi", "/s"}

func apiOwned(p string) bool {
	for _, prefix := range apiPrefixes {
		if p == prefix || strings.HasPrefix(p, prefix+"/") {
			return true
		}
	}
	return false
}

// Handler serves a GET or HEAD that no API route claimed: a file from the
// bundle, or index.html for a client-side route.
func (a *WebApp) Handler() http.Handler {
	return a.compress(http.HandlerFunc(a.serve))
}

func (a *WebApp) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	p := path.Clean("/" + r.URL.Path)
	if apiOwned(p) {
		http.NotFound(w, r)
		return
	}
	name := strings.TrimPrefix(p, "/")
	if name != "" && name != "index.html" {
		if info, err := fs.Stat(a.root, name); err == nil && info.Mode().IsRegular() {
			if strings.HasPrefix(name, "assets/") {
				// Content-hashed, so these can never go stale.
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			http.ServeFileFS(w, r, a.root, name)
			return
		}
		// A missing asset is a missing asset, not a client-side route.
		if strings.HasPrefix(name, "assets/") {
			http.NotFound(w, r)
			return
		}
	}
	a.serveIndex(w, r)
}

// serveIndex writes the app shell. It is never cached, or a deploy would be
// invisible until the browser happened to revalidate.
func (a *WebApp) serveIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-cache, must-revalidate")
	http.ServeContent(w, r, "index.html", a.indexTime, bytes.NewReader(a.index))
}

// shareLanding answers a browser navigation to /s/<token> with the app shell.
//
// One path, two meanings: the API answers GET /s/<token> as JSON, and the SPA
// has a page at the same URL. A top-level navigation sends Accept: text/html
// and gets the page; the SPA's own fetch() does not, and gets the JSON.
// Sub-paths (/download, /children, …) are always the API.
func (a *WebApp) shareLanding(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if (r.Method == http.MethodGet || r.Method == http.MethodHead) &&
			isShareToken(r.URL.Path) &&
			strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/html") {
			a.serveIndex(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isShareToken reports whether p is exactly /s/<segment>.
func isShareToken(p string) bool {
	rest, ok := strings.CutPrefix(p, "/s/")
	return ok && rest != "" && !strings.Contains(rest, "/")
}

// Secure adds the security response headers to every response.
//
// The API gets them too, as it did behind nginx: a download is the one
// response that carries attacker-chosen bytes, and a CSP on it is worth
// having. Two directives are looser than the value every guide recommends,
// and tightening them breaks the app:
//
//   - object-src 'self', not 'none': the PDF viewer is an <object> pointed at
//     /api/v1/nodes/{id}/content.
//   - frame-src and form-action name the Document Server: the editor handoff
//     is a cross-origin form POST into an iframe.
//
// style-src 'unsafe-inline' is the one that needs front-end changes to drop
// (React style props, sonner's runtime <style>). script-src needs no nonce:
// the built index.html has no inline script.
func (a *WebApp) Secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		if a.hsts {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		h.Set("Content-Security-Policy", a.csp(r.Host))
		next.ServeHTTP(w, r)
	})
}

func (a *WebApp) csp(host string) string {
	// connect-src names this host's own socket origin for the collaboration
	// socket: CSP3 lets 'self' cover it, but older WebKit does not match a
	// ws(s) URL against 'self'. The Host header is the caller's to choose,
	// so one that is not a plain host[:port] is left out rather than spliced
	// into the policy.
	connect := "'self'"
	if validCSPHost(host) {
		connect += " wss://" + host + " ws://" + host
	}
	frame := "'self'"
	form := "'self'"
	if a.frameSrc != "" {
		frame += " " + a.frameSrc
		form += " " + a.frameSrc
	}
	return "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
		"img-src 'self' data: blob:; media-src 'self' blob:; object-src 'self'; " +
		"frame-src " + frame + "; form-action " + form + "; " +
		"frame-ancestors 'none'; base-uri 'none'; connect-src " + connect
}

// validCSPHost accepts a hostname, an IPv4 address or a bracketed IPv6
// address, with an optional port — and nothing that could end a directive.
func validCSPHost(h string) bool {
	if h == "" {
		return false
	}
	u, err := url.Parse("//" + h)
	if err != nil || u.Host != h || u.User != nil {
		return false
	}
	for _, c := range h {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.', c == '-', c == ':', c == '[', c == ']':
		default:
			return false
		}
	}
	return true
}
