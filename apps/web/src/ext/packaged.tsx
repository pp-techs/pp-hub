import { lazy, type SVGProps } from "react";
import { i18n } from "@lingui/core";
import type { MessageDescriptor } from "@lingui/core";
import type { ExtEditorProps, ExtManifest, IconComponent } from "./types";
import { Puzzle } from "lucide-react";
import { cn } from "@workspace/ui/lib/utils";
import type { Extension } from "@/lib/api";

// A packaged app (M51) as the rest of the SPA sees an extension: an
// `ExtManifest` built from what `GET /extensions` says about it. Its strings
// are the manifest's, translated by the manifest's own `translations`; its
// icon is a file of the package; its editor is the sandboxed frame.

/** The catalog id of one of an app's strings. */
function stringId(appId: string, english: string): string {
  // eslint-disable-next-line lingui/no-unlocalized-strings -- catalog id
  return `pp-app:${appId}:${english}`;
}

function descriptor(appId: string, english: string): MessageDescriptor {
  return { id: stringId(appId, english), message: english };
}

/**
 * A manifest string as Lingui's compiled catalogs hold it: text, with
 * `{actor}` and `{excerpt}` as placeholders. Nothing else in it is ICU, so a
 * brace or an apostrophe stays literal.
 */
function compile(text: string): (string | [string])[] {
  return text
    .split(/(\{actor\}|\{excerpt\})/)
    .filter((part) => part !== "")
    .map((part) => (part === "{actor}" || part === "{excerpt}" ? [part.slice(1, -1)] : part));
}

let apps: readonly Extension[] = [];
let loadedFor = new WeakSet<object>();

function appStrings(app: Extension): string[] {
  const p = app.package;
  return [
    app.name,
    ...p.kinds.flatMap((k) => [k.label, k.new_label, k.default_name, k.new_hint]),
    ...Object.values(p.notifications),
  ].filter((s) => s !== "");
}

/**
 * Puts the apps' strings into the active locale's catalog. Run when the set
 * of apps changes and whenever the catalog does: `activateLocale` replaces
 * the catalog object, which drops them.
 */
function loadAppStrings() {
  const messages = i18n.messages as object;
  if (loadedFor.has(messages) || apps.length === 0) return;
  const locale = i18n.locale;
  const base = locale.split("-")[0];
  const add: Record<string, (string | [string])[]> = {};
  for (const app of apps) {
    const tr = app.package.translations;
    const table = tr[locale] ?? (base ? tr[base] : undefined) ?? {};
    for (const english of appStrings(app)) {
      add[stringId(app.id, english)] = compile(table[english] ?? english);
    }
  }
  loadedFor.add(messages);
  i18n.load(locale, add);
  // `load` merges into the same object, unless the locale had none yet.
  loadedFor.add(i18n.messages as object);
}

// eslint-disable-next-line lingui/no-unlocalized-strings -- event name
i18n.on("change", loadAppStrings);

/** Registers the packaged apps' strings; the registry calls it when the set changes. */
export function syncAppStrings(list: readonly Extension[]): void {
  apps = list;
  loadedFor = new WeakSet();
  loadAppStrings();
}

/**
 * An icon from the package. An SVG is drawn as a mask in the current text
 * colour, so it sits among the app's own glyphs like one of them; a PNG is
 * drawn as it is.
 */
function packageIcon(url: string): IconComponent {
  if (!url) return Puzzle;
  if (/\.svg($|\?)/i.test(url)) {
    const Mask = ({ className }: SVGProps<SVGSVGElement>) => (
      <span
        aria-hidden
        className={cn("inline-block size-4 shrink-0 bg-current", className)}
        style={{
          maskImage: `url("${url}")`,
          WebkitMaskImage: `url("${url}")`,
          maskSize: "contain",
          WebkitMaskSize: "contain",
          maskRepeat: "no-repeat",
          WebkitMaskRepeat: "no-repeat",
          maskPosition: "center",
          WebkitMaskPosition: "center",
        }}
      />
    );
    return Mask;
  }
  const Img = ({ className }: SVGProps<SVGSVGElement>) => (
    <img
      src={url}
      alt=""
      aria-hidden
      className={cn("inline-block size-4 shrink-0 object-contain", className)}
    />
  );
  return Img;
}

/** The `ExtManifest` of an uploaded app. */
export function packagedManifest(app: Extension): ExtManifest {
  const p = app.package;
  // `editor` is a lazy component by contract (ExtNodeKind): the frame and its
  // bridge load with the first app item opened, like every app's editor.
  const Frame = lazy(async () => {
    const { AppFrame } = await import("./app-frame");
    return {
      default: (props: ExtEditorProps) => <AppFrame app={app} {...props} />,
    };
  });
  const notifications: ExtManifest["notifications"] = {};
  for (const verb of Object.keys(p.notifications)) {
    const d = descriptor(app.id, p.notifications[verb]!);
    notifications[verb] = ({ actor, excerpt }) => ({ ...d, values: { actor, excerpt } });
  }
  return {
    id: app.id,
    name: descriptor(app.id, app.name),
    nodeKinds: p.kinds.map((k) => ({
      kind: k.kind,
      label: descriptor(app.id, k.label),
      icon: packageIcon(k.icon_url || p.icon_url),
      newLabel: descriptor(app.id, k.new_label),
      defaultName: descriptor(app.id, k.default_name),
      newHint: k.new_hint ? descriptor(app.id, k.new_hint) : undefined,
      editor: Frame,
    })),
    notifications,
  };
}
