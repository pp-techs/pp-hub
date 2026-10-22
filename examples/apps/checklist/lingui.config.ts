import { defineConfig } from "@lingui/cli";
import { formatter } from "@lingui/format-po";

// The app's own catalogs (en + vi), compiled into the bundle by
// `@lingui/vite-plugin`. Refresh them with `vp run i18n:extract` after changing
// a string. Origins without line numbers, so moving code does not drift them.
export default defineConfig({
  sourceLocale: "en",
  locales: ["en", "vi"],
  fallbackLocales: { default: "en" },
  catalogs: [
    {
      path: "<rootDir>/src/locales/{locale}/messages",
      include: ["<rootDir>/src"],
      exclude: ["**/node_modules/**", "**/*.test.*"],
    },
  ],
  format: formatter({ origins: true, lineNumbers: false }),
  compileNamespace: "es",
  orderBy: "message",
});
