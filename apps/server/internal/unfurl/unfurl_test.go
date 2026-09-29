package unfurl

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func TestPublicAddr(t *testing.T) {
	for addr, want := range map[string]bool{
		"93.184.215.14":            true,
		"8.8.8.8":                  true,
		"2606:4700:4700::1111":     true,
		"127.0.0.1":                false,
		"127.8.9.10":               false,
		"::1":                      false,
		"10.1.2.3":                 false,
		"172.16.0.1":               false,
		"192.168.1.1":              false,
		"169.254.169.254":          false, // AWS/GCP/Azure metadata
		"100.100.100.200":          false, // Alibaba metadata (CGNAT range)
		"fd00:ec2::254":            false, // AWS IPv6 metadata
		"fe80::1":                  false,
		"0.0.0.0":                  false,
		"::":                       false,
		"224.0.0.1":                false,
		"255.255.255.255":          false,
		"::ffff:127.0.0.1":         false, // IPv4-mapped loopback
		"::ffff:169.254.169.254":   false,
		"64:ff9b::a9fe:a9fe":       false, // NAT64 of the metadata address
		"2002:a9fe:a9fe::1":        false, // 6to4 of it
		"198.51.100.7":             false,
		"2001:db8::1":              false,
		"fc00::1":                  false,
		"240.1.2.3":                false,
		"192.0.0.170":              false,
		"2001:0:4136:e378::1":      false, // Teredo
		"64:ff9b:1::a00:1":         false,
		"100::1":                   false,
		"198.18.0.1":               false,
		"203.0.113.9":              false,
		"192.0.2.1":                false,
		"0.1.2.3":                  false,
		"::ffff:8.8.8.8":           true,
		"2a00:1450:4001:82b::200e": true,
	} {
		if got := PublicAddr(netip.MustParseAddr(addr)); got != want {
			t.Errorf("PublicAddr(%s) = %v, want %v", addr, got, want)
		}
	}
}

// loopbackOnly is the test seam: an httptest server lives on 127.0.0.1,
// which production refuses.
func loopbackOnly(a netip.Addr) bool { return a == netip.MustParseAddr("127.0.0.1") }

// With the production policy nothing on this machine is reachable — not by
// literal address, and not through a hostname that resolves to one, which is
// refused when the connection is dialled.
func TestPageRefusesNonPublicDestinations(t *testing.T) {
	hits := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<title>internal</title>"))
	}))
	defer ts.Close()
	port := ts.Listener.Addr().String()
	port = port[strings.LastIndexByte(port, ':'):]

	f := New(Config{})
	ctx := context.Background()
	for _, raw := range []string{
		ts.URL,                          // 127.0.0.1 literal
		"http://localhost" + port + "/", // resolved at dial time
		"http://[::1]" + port + "/",     // IPv6 loopback
		"http://169.254.169.254/latest/meta-data/",
		"http://[::ffff:127.0.0.1]" + port + "/",
		"http://10.0.0.1/",
	} {
		if _, err := f.Page(ctx, raw); !errors.Is(err, ErrForbiddenAddress) {
			t.Errorf("Page(%s): %v, want ErrForbiddenAddress", raw, err)
		}
	}
	if hits != 0 {
		t.Fatalf("the internal server was reached %d times", hits)
	}
	for _, raw := range []string{"file:///etc/passwd", "gopher://x/", "ftp://example.com/", "//example.com/x", "not a url", "", "http://user:pw@example.com/"} {
		if _, err := f.Page(ctx, raw); !errors.Is(err, ErrInvalidURL) {
			t.Errorf("Page(%q): %v, want ErrInvalidURL", raw, err)
		}
	}
}

// A redirect is dialled like the first request: a permitted page that
// redirects somewhere private gets nowhere.
func TestPageRefusesRedirectToPrivate(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/metadata":
			http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
		case "/v6":
			http.Redirect(w, r, "http://[::1]:"+r.URL.Port()+"/", http.StatusFound)
		case "/scheme":
			http.Redirect(w, r, "file:///etc/passwd", http.StatusFound)
		case "/loop":
			http.Redirect(w, r, "/loop", http.StatusFound)
		}
	}))
	defer ts.Close()
	f := New(Config{AllowAddr: loopbackOnly})
	ctx := context.Background()
	for path, want := range map[string]error{
		"/metadata": ErrForbiddenAddress,
		"/v6":       ErrForbiddenAddress,
		"/scheme":   ErrInvalidURL,
	} {
		if _, err := f.Page(ctx, ts.URL+path); !errors.Is(err, want) {
			t.Errorf("redirect %s: %v, want %v", path, err, want)
		}
	}
	if _, err := f.Page(ctx, ts.URL+"/loop"); err == nil || !strings.Contains(err.Error(), "redirects") {
		t.Errorf("redirect loop: %v, want the redirect limit", err)
	}
}

func onePixelPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 10, G: 20, B: 30, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// Allowed through the seam, a local page is read like any other: OpenGraph
// first, then Twitter cards, then <title>/description; relative image URLs
// resolve against the page after redirects; a non-HTML link is just its URL.
func TestPageReadsMetadata(t *testing.T) {
	pngBytes := onePixelPNG(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			http.Redirect(w, r, "/articles/one", http.StatusMovedPermanently)
		case "/articles/one":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<!doctype html><html><head>
<title>Fallback title</title>
<meta property="og:title" content="  The   real title ">
<meta name="twitter:description" content="From the card">
<meta name="description" content="Plain description">
<meta property="og:image" content="../img/cover.png">
</head><body><title>not this</title><meta property="og:description" content="body meta"></body></html>`))
		case "/plain":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<html><head><title>Only &amp; a title</title><meta name="description" content="Desc"></head></html>`))
		case "/latin1":
			w.Header().Set("Content-Type", "text/html; charset=windows-1252")
			_, _ = w.Write([]byte("<title>Caf\xe9</title>"))
		case "/file.pdf":
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte("%PDF-1.4"))
		case "/img/cover.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(pngBytes)
		case "/huge":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<title>Huge</title>"))
			_, _ = w.Write(bytes.Repeat([]byte("x"), 3<<20))
		case "/gone":
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()
	f := New(Config{AllowAddr: loopbackOnly})
	ctx := context.Background()

	page, err := f.Page(ctx, ts.URL+"/start#frag")
	if err != nil {
		t.Fatal(err)
	}
	want := Page{URL: ts.URL + "/articles/one", Title: "The real title", Description: "From the card", Image: ts.URL + "/img/cover.png"}
	if page != want {
		t.Fatalf("page %+v, want %+v", page, want)
	}
	img, err := f.Image(ctx, page.Image, 0)
	if err != nil || img.Mime != "image/png" || img.Ext != ".png" || !bytes.Equal(img.Body, pngBytes) {
		t.Fatalf("image: %v %q %q %d bytes", err, img.Mime, img.Ext, len(img.Body))
	}

	for path, want := range map[string]Page{
		"/plain":    {Title: "Only & a title", Description: "Desc"},
		"/latin1":   {Title: "Café"},
		"/file.pdf": {},
		"/huge":     {Title: "Huge"},
	} {
		got, err := f.Page(ctx, ts.URL+path)
		want.URL = ts.URL + path
		if err != nil || got != want {
			t.Errorf("%s: %+v (%v), want %+v", path, got, err, want)
		}
	}
	if _, err := f.Page(ctx, ts.URL+"/gone"); err == nil {
		t.Error("a 404 page unfurled")
	}
}

// A preview image must be declared and sniffed as a raster image, and fit.
func TestImageChecksTypeAndSize(t *testing.T) {
	pngBytes := onePixelPNG(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/svg":
			w.Header().Set("Content-Type", "image/svg+xml")
			_, _ = w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`))
		case "/lying":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("<html><script>alert(1)</script></html>"))
		case "/html":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write(pngBytes)
		case "/ok":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(pngBytes)
		}
	}))
	defer ts.Close()
	f := New(Config{AllowAddr: loopbackOnly})
	ctx := context.Background()
	for path, want := range map[string]error{"/svg": ErrNotImage, "/lying": ErrNotImage, "/html": ErrNotImage} {
		if _, err := f.Image(ctx, ts.URL+path, 0); !errors.Is(err, want) {
			t.Errorf("%s: %v, want %v", path, err, want)
		}
	}
	if _, err := f.Image(ctx, ts.URL+"/ok", int64(len(pngBytes)-1)); !errors.Is(err, ErrTooLarge) {
		t.Errorf("over the limit: %v, want ErrTooLarge", err)
	}
	if _, err := New(Config{}).Image(ctx, ts.URL+"/ok", 0); !errors.Is(err, ErrForbiddenAddress) {
		t.Errorf("production policy fetched a loopback image: %v", err)
	}
}
