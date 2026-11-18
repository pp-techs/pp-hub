/**
 * A sandboxed frame without allow-same-origin makes every read of
 * localStorage/sessionStorage throw, which aborts the script of any page that
 * reads them unguarded at load (slide decks, dashboards). This swaps in an
 * in-memory store when, and only when, the real one throws. The server injects
 * the same script into published pages (apps/server/internal/httpapi/artifact_shim.go);
 * keep the two in step.
 */
const STORAGE_SHIM = `<script>try{localStorage.length}catch(e){["localStorage","sessionStorage"].forEach(function(n){var m=new Map;Object.defineProperty(window,n,{configurable:true,value:{getItem:function(k){k=String(k);return m.has(k)?m.get(k):null},setItem:function(k,v){m.set(String(k),String(v))},removeItem:function(k){m.delete(String(k))},clear:function(){m.clear()},key:function(i){var a=Array.from(m.keys());return i<a.length?a[i]:null},get length(){return m.size}}})})}</script>`;

const INSERT_AFTER = [/<head(?:\s[^>]*)?>/i, /<html(?:\s[^>]*)?>/i, /^\s*<!doctype[^>]*>/i];

/** Places the shim as early as possible without leaving standards mode. */
export function withStorageShim(html: string): string {
  for (const re of INSERT_AFTER) {
    const m = re.exec(html);
    if (m) {
      const at = m.index + m[0].length;
      return html.slice(0, at) + STORAGE_SHIM + html.slice(at);
    }
  }
  return STORAGE_SHIM + html;
}
