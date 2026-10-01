import { useEffect } from "react";
import { titleWithProduct } from "@/lib/i18n";

/**
 * Sets the tab title to `<name> - PP Hub` once `name` is known. Until then the
 * route's static `head` title stands. Routes whose title depends on loaded data
 * call this; `head` runs outside React and cannot read the query cache reactively.
 */
export function useDocumentTitle(name: string | undefined): void {
  useEffect(() => {
    if (name) document.title = titleWithProduct(name);
  }, [name]);
}

/**
 * Swaps the tab icon for `href` while mounted. The static icon links in
 * `index.html` are detached meanwhile — with several candidates the browser
 * picks by size, not by order — and put back on cleanup.
 */
export function useFavicon(href: string | undefined): void {
  useEffect(() => {
    if (!href) return;
    const statics = Array.from(document.head.querySelectorAll<HTMLLinkElement>('link[rel="icon"]'));
    const placeholders = statics.map((link) => {
      // eslint-disable-next-line lingui/no-unlocalized-strings -- DOM comment, not UI text
      const marker = document.createComment("favicon");
      link.replaceWith(marker);
      return { link, marker };
    });
    // eslint-disable-next-line lingui/no-unlocalized-strings -- DOM tag name
    const dynamic = document.createElement("link");
    dynamic.rel = "icon";
    dynamic.type = "image/svg+xml";
    dynamic.href = href;
    document.head.appendChild(dynamic);
    return () => {
      dynamic.remove();
      for (const { link, marker } of placeholders) marker.replaceWith(link);
    };
  }, [href]);
}
