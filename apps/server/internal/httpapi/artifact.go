package httpapi

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/nhatbui/pp-hub/apps/server/internal/publish"
	"github.com/nhatbui/pp-hub/apps/server/internal/store"
)

// Published sites (M50) are served by this process on PP_ADDR, from one of
// two places:
//
//   - PP_ARTIFACT_URL's origin, told apart by Host — an origin of its own, so
//     a page that somehow escaped its sandbox still reaches no session;
//   - with no artifact origin, this origin under /a/ (publish.SamePath),
//     where the sandboxing CSP is the only isolation.
//
// Either way everything answered is a published snapshot's bytes under that
// CSP — no /api, /auth, /mcp, /dav, /s, no app, no session, no cookie read or
// set. TestArtifactHostServesNothingElse holds both directions.

// artifactSwitch hands a request for published bytes to art and nothing
// else; any other request goes on to the main router. It is the main
// router's first middleware, so none of the others — the app's security
// headers included (X-Frame-Options: DENY would stop the viewer framing a
// page) — touch an artifact response.
func (s *Server) artifactSwitch(art http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if art == nil {
			return next
		}
		if s.artifactHost == "" {
			same := http.StripPrefix(publish.SamePath, art)
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, publish.SamePath+"/") {
					same.ServeHTTP(w, r)
					return
				}
				next.ServeHTTP(w, r)
			})
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.EqualFold(r.Host, s.artifactHost) {
				art.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// artifactRouter is the router for published bytes, or nil when publishing
// has nowhere to serve from.
func (s *Server) artifactRouter() http.Handler {
	if s.publish.ArtifactURL() == "" {
		return nil
	}
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	if len(s.trustedProxies) > 0 {
		r.Use(middleware.ClientIPFromXFF(s.trustedProxies...))
	}
	// A per-IP limit, like /s/: a token is a guessable secret in a URL.
	// Wider than /s/ because one page load fetches its scripts, styles and
	// images through it too.
	limit := newLimiter(600, 200)
	r.Use(s.observeArtifact, recoverer, s.artifactHeaders, limit.middleware)
	compress := middleware.Compress(5,
		"text/html", "text/css", "text/plain", "text/markdown", "text/csv",
		"text/javascript", "application/javascript", "application/json",
		"image/svg+xml", "application/xml", "text/xml", "application/wasm")
	serve := s.serveArtifact(compress)
	r.Get("/*", serve)
	r.Head("/*", serve)
	r.NotFound(http.NotFound)
	r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Allow", "GET, HEAD")
		w.WriteHeader(http.StatusMethodNotAllowed)
	})
	return r
}

// artifactHeaders sets the sandbox on every artifact response — a 404, a
// redirect and a 429 included — before anything else can write.
func (s *Server) artifactHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", s.artifactCSP())
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// artifactCSP is the policy of publishing.md. The artifact origin is named
// rather than 'self', which an opaque-origin (sandboxed) document may not
// match; the CDN list is the admin's, already validated as bare https
// origins.
func (s *Server) artifactCSP() string {
	art := s.publish.ArtifactURL()
	if !s.publish.Isolated() {
		// A source with a path matches that path exactly unless it ends in
		// "/", so the prefix must keep its slash to cover the site's files.
		art += "/"
	}
	cdn := ""
	if s.sysconfig != nil {
		if list := s.sysconfig.ArtifactCDNAllowlist(); len(list) > 0 {
			cdn = " " + strings.Join(list, " ")
		}
	}
	return "sandbox allow-scripts allow-popups allow-popups-to-escape-sandbox allow-modals allow-downloads; " +
		"default-src 'none'; " +
		"script-src " + art + " 'unsafe-inline' 'unsafe-eval'" + cdn + "; " +
		"style-src " + art + " 'unsafe-inline'" + cdn + "; " +
		"img-src " + art + " data: blob:" + cdn + "; " +
		"font-src " + art + " data:" + cdn + "; " +
		"media-src " + art + " data: blob:; " +
		"connect-src 'none'; form-action 'none'; base-uri " + art + "; frame-src 'none'; " +
		"frame-ancestors " + OriginOf(s.externalURL)
}

// serveArtifact answers GET/HEAD /<token>/[~<grant>/]<path>.
func (s *Server) serveArtifact(compress func(http.Handler) http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.EscapedPath(), "/")
		token, after, hasSlash := strings.Cut(rest, "/")
		sv, err := s.publish.Resolve(r.Context(), token)
		if err != nil {
			if !errors.Is(err, publish.ErrNotFound) {
				slog.ErrorContext(r.Context(), "artifact resolve", "err", err)
			}
			http.NotFound(w, r)
			return
		}
		if !hasSlash {
			// Relative URLs in the entry page resolve against a directory.
			redirectWithSlash(w, r)
			return
		}
		grant := ""
		if g, ok := strings.CutPrefix(after, "~"); ok {
			gv, tail, ok := strings.Cut(g, "/")
			if !ok {
				redirectWithSlash(w, r)
				return
			}
			grant, after = gv, tail
		}
		if sv.NeedsPassword() && !s.publish.CheckGrant(sv.Publication, grant) {
			http.NotFound(w, r)
			return
		}
		p, dir, ok := publish.CleanPath(after)
		if !ok {
			http.NotFound(w, r)
			return
		}
		name := p
		if dir {
			name = path.Join(p, "index.html")
			if p == "" {
				name = "index.html"
			}
		}
		f, rc, err := s.publish.Open(r.Context(), sv, name)
		if errors.Is(err, publish.ErrNotFound) && !dir {
			// A folder named without its slash: send the browser to it, so
			// its page's relative URLs resolve inside it.
			if _, idx, err := s.publish.Open(r.Context(), sv, path.Join(p, "index.html")); err == nil {
				_ = idx.Close()
				redirectWithSlash(w, r)
				return
			}
		}
		if errors.Is(err, publish.ErrNotFound) {
			s.serveArtifactMissing(w, r, sv)
			return
		}
		if err != nil {
			Error(w, r, err)
			return
		}
		defer rc.Close()
		h := func(w http.ResponseWriter, r *http.Request) {
			writeArtifactHeaders(w, f)
			rs, ok := rc.(io.ReadSeeker)
			if !ok {
				rs = &forwardSeeker{r: rc, size: f.Size}
			}
			http.ServeContent(w, r, "", sv.Publication.PublishedAt.Time, rs)
		}
		// A range is a slice of the stored bytes; compressing it would make
		// it a slice of something else.
		if r.Header.Get("Range") == "" {
			compress(http.HandlerFunc(h)).ServeHTTP(w, r)
			return
		}
		h(w, r)
	}
}

func writeArtifactHeaders(w http.ResponseWriter, f store.PublicationFile) {
	h := w.Header()
	h.Set("Content-Type", publish.ContentType(f))
	h.Set("ETag", `"`+f.BlobHash+`"`)
	// Revalidate every time: a 304 is cheap, and an unpublish or a
	// republish then takes effect on the next request.
	h.Set("Cache-Control", "private, no-cache")
}

// serveArtifactMissing answers a path the snapshot does not hold: its
// 404.html with status 404 when it has one, a bare 404 otherwise.
func (s *Server) serveArtifactMissing(w http.ResponseWriter, r *http.Request, sv publish.Served) {
	f, rc, err := s.publish.Open(r.Context(), sv, "404.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer rc.Close()
	writeArtifactHeaders(w, f)
	w.Header().Del("ETag")
	w.WriteHeader(http.StatusNotFound)
	if r.Method != http.MethodHead {
		_, _ = io.Copy(w, rc)
	}
}

// redirectWithSlash sends the browser to the same path with a trailing
// slash. Found, not Moved Permanently: a cached permanent redirect would
// outlive an unpublish. The Location is relative to the request's last
// segment, because under /a/ the router sees the path with the prefix
// stripped — and http.Redirect would resolve a relative target against
// that stripped path.
func redirectWithSlash(w http.ResponseWriter, r *http.Request) {
	p := r.URL.EscapedPath()
	target := "./"
	if i := strings.LastIndexByte(p, '/'); i < len(p)-1 {
		target = p[i+1:] + "/"
	}
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	w.Header().Set("Location", target)
	w.WriteHeader(http.StatusFound)
}
