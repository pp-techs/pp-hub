import path from "node:path";
import { fileURLToPath } from "node:url";
import { lingui } from "@lingui/vite-plugin";
import { tanstackRouter } from "@tanstack/router-plugin/vite";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import Icons from "unplugin-icons/vite";
import { defineConfig } from "vite-plus";

const root = path.dirname(fileURLToPath(import.meta.url));

export default defineConfig({
  plugins: [
    tanstackRouter({ target: "react", autoCodeSplitting: true }),
    // `@vitejs/plugin-react` deliberately ignores the project's Babel config,
    // so the Lingui macro has to be named here. It rewrites `<Trans>`/`t` into
    // `i18n._({id, message})` with an id hashed from the source text.
    react({ babel: { plugins: ["@lingui/babel-plugin-lingui-macro"] } }),
    tailwindcss(),
    // vscode-icons file glyphs, compiled to inline SVG components at build
    // time so the drive never calls out to the Iconify API.
    Icons({ compiler: "jsx", jsx: "react" }),
    // Compiles `src/locales/*/messages.po` during transform, so the bundle
    // carries pre-compiled message trees and no runtime ICU parser.
    lingui({ failOnCompileError: true }),
  ],
  resolve: {
    alias: {
      "@": path.resolve(root, "./src"),
    },
  },
  server: {
    proxy: {
      // `ws: true` carries the block editor's collab socket
      // (`/api/v1/collab/{room}`) through the same entry. The Origin header
      // passes through untouched — the server compares it to
      // PP_EXTERNAL_URL, which in dev is this server's address.
      "/api": { target: "http://localhost:8080", ws: true },
      "/auth": "http://localhost:8080",
      // Logto webhook deliveries. Not under /api on purpose — that group is
      // authenticated and same-origin-checked — so it needs its own entry
      // here and its own `location` in nginx.conf.
      "/hooks": "http://localhost:8080",
      // Agents over MCP (M41) and the metadata they read before signing in.
      // Outside /api for the same reason as /hooks: bearer-only, no cookie,
      // no same-origin check. Mirrored by locations in nginx.conf.
      "^/mcp(/|$)": "http://localhost:8080",
      "/.well-known/oauth-protected-resource": "http://localhost:8080",
      // Share links. NOTE: a bare "/s" prefix entry breaks this core's
      // module middleware; the "^/s/" context form works.
      // Top-level browser navigations to /s/<token> (Accept: text/html)
      // fall through to the SPA route; the SPA's fetch() and
      // /s/<token>/download go to the server.
      "^/s/": {
        target: "http://localhost:8080",
        bypass(req) {
          if (req.headers.accept?.includes("text/html") && /^\/s\/[^/]+$/.test(req.url ?? "")) {
            return req.url;
          }
        },
      },
    },
  },
});
