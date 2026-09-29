// Package unfurl fetches a web page's title, description and preview image for
// a block document's link bookmark (the embed block).
//
// It is a server-side fetch of a URL a user typed, so it is built against
// server-side request forgery: the address is checked when the connection is
// dialled — after DNS, on every attempt and every redirect, so neither a
// hostname resolving to 127.0.0.1 nor a redirect to 169.254.169.254 nor DNS
// rebinding gets through — proxies from the environment are ignored (a proxy
// would dial on our behalf, unchecked), only http and https are followed, and
// every read is bounded in time and size.
package unfurl

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
)

var (
	// ErrInvalidURL refuses anything but an absolute http or https URL
	// without credentials.
	ErrInvalidURL = errors.New("the link must be an http or https address")
	// ErrForbiddenAddress refuses a destination on a private, loopback,
	// link-local, metadata or otherwise non-public address.
	ErrForbiddenAddress = errors.New("the link points to an address this server does not fetch")
	// ErrNotImage refuses a preview image that is not PNG, JPEG, GIF or WebP.
	ErrNotImage = errors.New("the preview image is not a supported image")
	// ErrTooLarge refuses a preview image over the size limit.
	ErrTooLarge = errors.New("the preview image is too large")
)

// Config is the fetch policy. The zero value of any field takes its default.
type Config struct {
	// AllowAddr decides whether an address may be dialled; nil is PublicAddr.
	// Only tests set it, to reach an httptest server on loopback.
	AllowAddr func(netip.Addr) bool
	// Timeout bounds each fetch end to end, redirects included (5 s).
	Timeout time.Duration
	// MaxHTMLBytes is how much of a page is read (1 MiB); the metadata is in
	// its head, so a longer page is parsed as far as that.
	MaxHTMLBytes int64
	// MaxImageBytes refuses a larger preview image (5 MiB).
	MaxImageBytes int64
	// MaxRedirects bounds a redirect chain (5).
	MaxRedirects int
}

// Fetcher fetches pages and images under a Config. Safe for concurrent use.
type Fetcher struct {
	cfg    Config
	client *http.Client
}

// Page is what a link's page says about itself. Any field but URL may be
// empty: a page without metadata, or a link that is not a page at all.
type Page struct {
	// URL is where the fetch ended up, after redirects.
	URL         string
	Title       string
	Description string
	// Image is the absolute URL of the page's preview image.
	Image string
}

// Image is a fetched preview image, its type sniffed from its bytes.
type Image struct {
	Body []byte
	Mime string
	// Ext is the file extension for Mime, dot included.
	Ext string
}

// New builds a fetcher.
func New(cfg Config) *Fetcher {
	if cfg.AllowAddr == nil {
		cfg.AllowAddr = PublicAddr
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Second
	}
	if cfg.MaxHTMLBytes <= 0 {
		cfg.MaxHTMLBytes = 1 << 20
	}
	if cfg.MaxImageBytes <= 0 {
		cfg.MaxImageBytes = 5 << 20
	}
	if cfg.MaxRedirects <= 0 {
		cfg.MaxRedirects = 5
	}
	f := &Fetcher{cfg: cfg}
	dialer := &net.Dialer{Timeout: cfg.Timeout, Control: f.control}
	transport := &http.Transport{
		// Never http.ProxyFromEnvironment: a proxy dials for us, and
		// control would only ever see the proxy's address.
		Proxy:                  nil,
		DialContext:            dialer.DialContext,
		ForceAttemptHTTP2:      true,
		TLSHandshakeTimeout:    cfg.Timeout,
		ResponseHeaderTimeout:  cfg.Timeout,
		MaxResponseHeaderBytes: 64 << 10,
		DisableKeepAlives:      true,
	}
	f.client = &http.Client{
		Transport: transport,
		Timeout:   cfg.Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= cfg.MaxRedirects {
				return fmt.Errorf("more than %d redirects", cfg.MaxRedirects)
			}
			return f.checkURL(req.URL)
		},
	}
	return f
}

// control runs once the address is resolved and before the connection is
// made: the one point where the address checked is the address dialled.
func (f *Fetcher) control(_, address string, _ syscall.RawConn) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil {
		return ErrForbiddenAddress
	}
	if !f.cfg.AllowAddr(ap.Addr().Unmap()) {
		return ErrForbiddenAddress
	}
	return nil
}

// checkURL refuses what is not worth dialling: another scheme, credentials,
// or a literal address that control would refuse anyway (answered here so
// the caller hears why, rather than a dial error).
func (f *Fetcher) checkURL(u *url.URL) error {
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return ErrInvalidURL
	}
	if addr, err := netip.ParseAddr(strings.Trim(u.Hostname(), "[]")); err == nil && !f.cfg.AllowAddr(addr.Unmap()) {
		return ErrForbiddenAddress
	}
	return nil
}

// ParseURL parses a link as typed: an absolute http or https URL.
func ParseURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 2048 {
		return nil, ErrInvalidURL
	}
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() {
		return nil, ErrInvalidURL
	}
	u.Fragment = ""
	return u, nil
}

// get fetches u, answering the response of a 2xx or an error.
func (f *Fetcher) get(ctx context.Context, u *url.URL, accept string) (*http.Response, error) {
	if err := f.checkURL(u); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, ErrInvalidURL
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", "pp-hub-unfurl/1 (+link preview)")
	resp, err := f.client.Do(req)
	if err != nil {
		// url.Error and net.OpError both unwrap, so a refusal in control or
		// CheckRedirect keeps its identity through them.
		for _, known := range []error{ErrForbiddenAddress, ErrInvalidURL} {
			if errors.Is(err, known) {
				return nil, known
			}
		}
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		resp.Body.Close()
		return nil, fmt.Errorf("%s answered %s", u.Host, resp.Status)
	}
	return resp, nil
}

// Page fetches raw and reads its metadata: OpenGraph first, then Twitter
// cards, then <title> and <meta name="description">. A response that is not
// HTML is a page with only its URL.
func (f *Fetcher) Page(ctx context.Context, raw string) (Page, error) {
	u, err := ParseURL(raw)
	if err != nil {
		return Page{}, err
	}
	resp, err := f.get(ctx, u, "text/html,application/xhtml+xml;q=0.9,*/*;q=0.1")
	if err != nil {
		return Page{}, err
	}
	defer resp.Body.Close()
	page := Page{URL: resp.Request.URL.String()}
	ct := resp.Header.Get("Content-Type")
	mt, _, _ := mime.ParseMediaType(ct)
	if mt != "text/html" && mt != "application/xhtml+xml" {
		return page, nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, f.cfg.MaxHTMLBytes))
	if err != nil {
		return Page{}, err
	}
	r, err := charset.NewReader(bytes.NewReader(body), ct)
	if err != nil {
		r = bytes.NewReader(body)
	}
	meta := readHead(r)
	page.Title = clip(first(meta, "og:title", "twitter:title", "<title>"), 300)
	page.Description = clip(first(meta, "og:description", "twitter:description", "description"), 500)
	if img := first(meta, "og:image:secure_url", "og:image", "og:image:url", "twitter:image", "twitter:image:src"); img != "" {
		if ref, err := resp.Request.URL.Parse(img); err == nil && (ref.Scheme == "http" || ref.Scheme == "https") {
			page.Image = ref.String()
		}
	}
	return page, nil
}

// imageTypes are the preview images accepted, by sniffed type. No SVG: it is
// a document that can script, and attachments render in place.
var imageTypes = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/gif":  ".gif",
	"image/webp": ".webp",
}

// Image fetches a preview image of at most limit bytes (the configured
// maximum when limit is 0 or larger). Both the declared type and the sniffed
// one must be a supported image.
func (f *Fetcher) Image(ctx context.Context, raw string, limit int64) (Image, error) {
	if limit <= 0 || limit > f.cfg.MaxImageBytes {
		limit = f.cfg.MaxImageBytes
	}
	u, err := ParseURL(raw)
	if err != nil {
		return Image{}, err
	}
	resp, err := f.get(ctx, u, "image/png,image/jpeg,image/gif,image/webp")
	if err != nil {
		return Image{}, err
	}
	defer resp.Body.Close()
	declared, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if _, ok := imageTypes[declared]; !ok {
		return Image{}, ErrNotImage
	}
	if resp.ContentLength > limit {
		return Image{}, ErrTooLarge
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return Image{}, err
	}
	if int64(len(body)) > limit {
		return Image{}, ErrTooLarge
	}
	sniffed := http.DetectContentType(body)
	ext, ok := imageTypes[sniffed]
	if !ok {
		return Image{}, ErrNotImage
	}
	return Image{Body: body, Mime: sniffed, Ext: ext}, nil
}

// readHead collects a page's metadata from its head: <title> under the key
// "<title>", each <meta> by its property or name, lowercased. The first
// value of a key wins. Reading stops at <body> or </head>.
func readHead(r io.Reader) map[string]string {
	meta := map[string]string{}
	z := html.NewTokenizer(r)
	inTitle := false
	var title strings.Builder
	for {
		switch z.Next() {
		case html.ErrorToken:
			return finishTitle(meta, title.String())
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			switch string(name) {
			case "body":
				return finishTitle(meta, title.String())
			case "title":
				inTitle = title.Len() == 0
			case "meta":
				var key, content string
				for hasAttr {
					var k, v []byte
					k, v, hasAttr = z.TagAttr()
					switch string(k) {
					case "property", "name":
						if key == "" {
							key = strings.ToLower(strings.TrimSpace(string(v)))
						}
					case "content":
						content = string(v)
					}
				}
				if key != "" && strings.TrimSpace(content) != "" {
					if _, seen := meta[key]; !seen {
						meta[key] = content
					}
				}
			}
		case html.TextToken:
			if inTitle {
				title.Write(z.Text())
			}
		case html.EndTagToken:
			name, _ := z.TagName()
			switch string(name) {
			case "title":
				inTitle = false
			case "head":
				return finishTitle(meta, title.String())
			}
		}
	}
}

func finishTitle(meta map[string]string, title string) map[string]string {
	if strings.TrimSpace(title) != "" {
		meta["<title>"] = title
	}
	return meta
}

func first(meta map[string]string, keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(meta[k]); v != "" {
			return v
		}
	}
	return ""
}

// clip collapses whitespace and cuts s to at most n runes.
func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:n-1])) + "…"
}

// PublicAddr reports whether a is a public unicast address: not loopback,
// private (RFC 1918, ULA), link-local (cloud metadata lives at
// 169.254.169.254 and fd00:ec2::254), shared (CGNAT, where Alibaba's
// metadata lives), multicast, unspecified, documentation, benchmarking or
// reserved — nor an IPv6 prefix that embeds an IPv4 address (NAT64, 6to4,
// Teredo), which could smuggle one of those through.
func PublicAddr(a netip.Addr) bool {
	a = a.Unmap()
	if !a.IsValid() || !a.IsGlobalUnicast() || a.IsPrivate() || a.IsLoopback() ||
		a.IsLinkLocalUnicast() || a.IsInterfaceLocalMulticast() || a.IsMulticast() || a.IsUnspecified() {
		return false
	}
	for _, p := range nonPublic {
		if p.Contains(a) {
			return false
		}
	}
	return true
}

var nonPublic = func() []netip.Prefix {
	var out []netip.Prefix
	for _, s := range []string{
		"0.0.0.0/8",       // "this network"
		"100.64.0.0/10",   // shared address space (CGNAT)
		"192.0.0.0/24",    // IETF protocol assignments
		"192.0.2.0/24",    // TEST-NET-1
		"198.18.0.0/15",   // benchmarking
		"198.51.100.0/24", // TEST-NET-2
		"203.0.113.0/24",  // TEST-NET-3
		"240.0.0.0/4",     // reserved, broadcast included
		"64:ff9b::/96",    // NAT64
		"64:ff9b:1::/48",  // local-use NAT64
		"100::/64",        // discard
		"2001::/32",       // Teredo
		"2001:db8::/32",   // documentation
		"2002::/16",       // 6to4
		"fc00::/7",        // unique local (IsPrivate covers it; kept explicit)
	} {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}()
