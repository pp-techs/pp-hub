import path from "node:path";
import { fileURLToPath } from "node:url";
import { tanstackRouter } from "@tanstack/router-plugin/vite";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import Icons from "unplugin-icons/vite";
import { defineConfig } from "vite-plus";

const root = path.dirname(fileURLToPath(import.meta.url));

export default defineConfig({
  plugins: [
    tanstackRouter({ target: "react", autoCodeSplitting: true }),
    react(),
    tailwindcss(),
    // vscode-icons file glyphs, compiled to inline SVG components at build
    // time so the drive never calls out to the Iconify API.
    Icons({ compiler: "jsx", jsx: "react" }),
  ],
  resolve: {
    alias: {
      "@": path.resolve(root, "./src"),
    },
  },
  server: {
    proxy: {
      "/api": "http://localhost:8080",
      "/auth": "http://localhost:8080",
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
