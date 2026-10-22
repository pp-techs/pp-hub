import { lingui } from "@lingui/vite-plugin";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite-plus";

// A packaged app: one static page served under a versioned asset path inside a
// sandboxed frame, so every URL in the build is relative (`base: "./"`) and the
// fonts are bundled rather than fetched.
export default defineConfig({
  base: "./",
  plugins: [
    // The Lingui macro rewrites `<Trans>`/`t` into `i18n._({id, message})`.
    react({ babel: { plugins: ["@lingui/babel-plugin-lingui-macro"] } }),
    tailwindcss(),
    // Compiles `src/locales/*/messages.po` during transform: no runtime ICU parser.
    lingui({ failOnCompileError: true }),
  ],
  resolve: { dedupe: ["react", "react-dom"] },
  // One page loaded once from the package: splitting the bundle buys nothing.
  build: { outDir: "dist", emptyOutDir: true, chunkSizeWarningLimit: 2048 },
});
