package httpapi

import "regexp"

// A published page runs in an opaque-origin sandbox (no allow-same-origin),
// where the browser makes every read of localStorage/sessionStorage throw a
// SecurityError. Pages written for a normal tab — slide decks, dashboards —
// often read them unguarded at load, which aborts their whole script. The
// shim swaps in an in-memory store for the lifetime of the page when, and only
// when, the real one throws; nothing persists and nothing reaches the origin.
const artifactStorageShim = `<script>try{localStorage.length}catch(e){["localStorage","sessionStorage"].forEach(function(n){var m=new Map;Object.defineProperty(window,n,{configurable:true,value:{getItem:function(k){k=String(k);return m.has(k)?m.get(k):null},setItem:function(k,v){m.set(String(k),String(v))},removeItem:function(k){m.delete(String(k))},clear:function(){m.clear()},key:function(i){var a=Array.from(m.keys());return i<a.length?a[i]:null},get length(){return m.size}}})})}</script>`

// artifactShimVersion is part of an injected page's ETag, so a change to the
// shim reaches browsers that cached the page.
const artifactShimVersion = "s1"

// artifactShimMaxBytes bounds the pages the shim is injected into: the page is
// held in memory to place it. A larger one is served untouched.
const artifactShimMaxBytes = 8 << 20

var (
	reHeadOpen = regexp.MustCompile(`(?i)<head(?:\s[^>]*)?>`)
	reHTMLOpen = regexp.MustCompile(`(?i)<html(?:\s[^>]*)?>`)
	reDoctype  = regexp.MustCompile(`(?i)^\s*<!doctype[^>]*>`)
)

// injectStorageShim returns page with the shim placed as early as possible
// without leaving standards mode: right after <head>, else <html>, else the
// doctype, else at the start.
func injectStorageShim(page []byte) []byte {
	at := 0
	for _, re := range []*regexp.Regexp{reHeadOpen, reHTMLOpen, reDoctype} {
		if loc := re.FindIndex(page); loc != nil {
			at = loc[1]
			break
		}
	}
	out := make([]byte, 0, len(page)+len(artifactStorageShim))
	out = append(out, page[:at]...)
	out = append(out, artifactStorageShim...)
	out = append(out, page[at:]...)
	return out
}
