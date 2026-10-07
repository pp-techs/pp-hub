import { defineConfig } from "vite-plus";

export default defineConfig({
  staged: {
    "*": "vp check --fix",
  },
  fmt: {
    // Written by apps/web/scripts/mdoc-fixtures.ts; reformatting them would
    // make every regeneration a diff. The block-document templates are in the
    // exact form mdoc.Render writes (blockdoc's tests hold them to it).
    ignorePatterns: [
      "apps/server/internal/mdoc/testdata/**",
      "apps/server/internal/mdoc/emoji.json",
      "apps/server/internal/blockdoc/templates/**",
    ],
  },
  lint: {
    jsPlugins: [
      { name: "vite-plus", specifier: "vite-plus/oxlint-plugin" },
      "@shadcn/lint",
      // Oxlint has no native equivalent of no-unlocalized-strings; the Lingui
      // ESLint plugin runs unchanged as an Oxlint JS plugin. Oxlint does not
      // read a plugin's `configs`, so every rule is listed explicitly.
      "eslint-plugin-lingui",
    ],
    rules: {
      "vite-plus/prefer-vite-plus-imports": "error",
      // A macro call outside a component or hook is evaluated once, at import
      // time, before a locale exists — the single most common way a string
      // silently stops translating.
      "lingui/t-call-in-function": "error",
      "lingui/no-trans-inside-trans": "error",
      "lingui/no-expression-in-message": "error",
      "lingui/no-single-tag-to-translate": "warn",
      "lingui/no-single-variables-to-translate": "warn",
      // Needs project-specific setup, which is why the plugin ships it off by
      // default: without an ignore list every Tailwind class, shadcn variant
      // and route path reads as untranslated copy. The names below are all
      // machine-facing. Deliberately NOT ignored, because a reader sees them:
      // placeholder, title, alt, label, aria-label, description.
      "lingui/no-unlocalized-strings": [
        "warn",
        {
          ignoreNames: [
            // Styling and layout
            "className",
            "class",
            "style",
            "variant",
            "size",
            "side",
            "align",
            "orientation",
            "position",
            "fill",
            "stroke",
            "viewBox",
            // Identity and routing
            "key",
            "id",
            "htmlFor",
            "name",
            "to",
            "from",
            "href",
            "src",
            "rel",
            "target",
            "path",
            "queryKey",
            // Discriminators and option values inside action/choice records:
            // `group: "create"`, `value: "name:asc"`, `slug: "danger"`. The
            // label beside them is a descriptor; these are the machine half
            // of the same object.
            "group",
            "value",
            "slug",
            // Component behaviour tokens, not copy: a Button variant, a
            // Sidebar mode, a router preload strategy.
            "confirmVariant",
            "collapsible",
            "defaultPreload",
            // Semantics the browser reads, not the user
            "type",
            "role",
            "dir",
            "lang",
            "method",
            "accept",
            "autoComplete",
            "inputMode",
            "enterKeyHint",
            "charSet",
            "xmlns",
            { regex: { pattern: "^data-", flags: "" } },
            { regex: { pattern: "^aria-(?!label$|description$)", flags: "" } },
          ],
          ignoreFunctions: [
            // Developer-facing, never rendered.
            "console.*",
            "cn",
            "cva",
            // Machine values: cache keys, URLs, DOM queries, storage.
            "*.matchMedia",
            "*.querySelector",
            "*.querySelectorAll",
            "*.closest",
            "*.setAttribute",
            "*.getItem",
            "*.setItem",
            "*.removeItem",
            "URLSearchParams",
            "*.createObjectURL",
            "*.addEventListener",
            "*.removeEventListener",
            "*.getElementById",
            // Surface ids. `openPanel("versions")` and
            // `openSettings("danger")` name a place, not a label.
            "openPanel",
            "openSettings",
            "selectSettingsPane",
            // Route paths, modal ids and redirect targets — addresses, not copy.
            "createFileRoute",
            "createModal",
            "createRootRouteWithContext",
            "redirect",
          ],
        },
      ],
    },
    // `no-unlocalized-strings` only means something where a reader is looking.
    // Everywhere below, a literal string is machine-facing by design, and
    // leaving hundreds of unactionable warnings standing is how a team learns
    // to scroll past lint output.
    overrides: [
      {
        // Test fixtures are literal by nature: node names, ids and expected
        // output are data, and there is no reader to translate them for.
        files: ["**/tests/**", "**/*.test.ts", "**/*.test.tsx"],
        rules: { "lingui/no-unlocalized-strings": "off" },
      },
      {
        // Developer scripts run from a terminal (the mdoc golden-fixture
        // generator, the release version step): markdown samples, file paths
        // and commands, no UI.
        files: ["apps/web/scripts/**", "scripts/**"],
        rules: { "lingui/no-unlocalized-strings": "off" },
      },
      {
        // The transport layer and the cache-key registry. `lib/api` must not
        // import the i18n runtime at all — see frontend-conventions.md — and
        // what it holds is MIME types, URL paths, extensions and messages
        // only a developer ever reads.
        files: ["apps/web/src/lib/api/**", "apps/web/src/lib/queries/**"],
        rules: { "lingui/no-unlocalized-strings": "off" },
      },
      {
        // node-icon maps extensions to icon identifiers; user-agent returns
        // proper nouns (Chrome, macOS); theme-provider holds storage keys and
        // a media query; i18n.ts holds locale tags and the language names,
        // which are deliberately written in the language they name;
        // use-settings-hash holds the URL fragment vocabulary of the settings
        // dialog and nothing a reader ever sees.
        files: [
          "apps/web/src/components/node-icon.tsx",
          "apps/web/src/components/theme-provider.tsx",
          "apps/web/src/lib/user-agent.ts",
          "apps/web/src/lib/i18n.ts",
          "apps/web/src/lib/use-settings-hash.ts",
        ],
        rules: { "lingui/no-unlocalized-strings": "off" },
      },
      {
        // Shared packages render no UI of their own. The design system in
        // particular has no i18n dependency by design — its few reader-facing
        // strings are the English defaults of UiStringsProvider, which the app
        // overrides.
        files: ["packages/**"],
        rules: { "lingui/no-unlocalized-strings": "off" },
      },
      {
        files: ["**/*.config.ts", "**/*.config.js"],
        rules: { "lingui/no-unlocalized-strings": "off" },
      },
    ],
    options: { typeAware: true, typeCheck: true },
  },
  run: {
    cache: true,
  },
});
