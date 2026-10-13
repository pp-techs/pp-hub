/* eslint-disable lingui/no-unlocalized-strings -- Mermaid theme variable names, CSS
   custom properties and DOM ids: nothing here is read by a person. */
import { useSyncExternalStore } from "react";

type ThemeVariables = Record<string, string | boolean>;

/** Mermaid theme variable → shadcn token it takes its colour from (text, lines, surfaces). */
const TOKEN_VARIABLES: Record<string, string> = {
  background: "--card",
  textColor: "--foreground",
  primaryTextColor: "--foreground",
  secondaryTextColor: "--foreground",
  tertiaryTextColor: "--foreground",
  nodeTextColor: "--foreground",
  lineColor: "--muted-foreground",
  clusterBorder: "--border",
  titleColor: "--foreground",
  edgeLabelBackground: "--card",
  noteTextColor: "--foreground",
  actorTextColor: "--foreground",
  signalColor: "--foreground",
  signalTextColor: "--foreground",
  labelTextColor: "--foreground",
  loopTextColor: "--foreground",
  pieStrokeColor: "--card",
  pieOuterStrokeColor: "--border",
  pieTitleTextColor: "--foreground",
  pieLegendTextColor: "--foreground",
  errorBkgColor: "--destructive",
  errorTextColor: "--foreground",
};

/** OKLCH hues of the categorical palette; the first slot is the brand colour itself. */
const ACCENT_HUES = [150, 70, 20, 300, 190, 100, 340, 220, 40, 270, 130];

/** Fill strength of a tinted surface, % of the accent over the card. */
const FILL_PERCENT = 14;

const tint = (color: string, percent: number) =>
  `color-mix(in srgb, ${color} ${percent}%, transparent)`;

/**
 * The shadcn neutrals alone render every diagram grey. The colourful part of
 * the palette comes from the brand (`--primary`) and a ring of fixed hues, so
 * nodes get a tinted fill with a saturated border, as on GitHub.
 */
function accentVariables(brand: string, dark: boolean): Record<string, string> {
  const lightness = dark ? 0.72 : 0.6;
  const chroma = dark ? 0.12 : 0.15;
  const accents = [brand, ...ACCENT_HUES.map((hue) => `oklch(${lightness} ${chroma} ${hue})`)];
  const [, green = brand, amber = brand] = accents;
  const variables: Record<string, string> = {
    primaryColor: tint(brand, FILL_PERCENT),
    primaryBorderColor: brand,
    secondaryColor: tint(green, FILL_PERCENT),
    secondaryBorderColor: green,
    tertiaryColor: tint(amber, FILL_PERCENT),
    tertiaryBorderColor: amber,
    mainBkg: tint(brand, FILL_PERCENT),
    nodeBorder: brand,
    clusterBkg: tint(amber, FILL_PERCENT / 2),
    noteBkgColor: tint(amber, FILL_PERCENT * 1.5),
    noteBorderColor: amber,
    actorBkg: tint(brand, FILL_PERCENT),
    actorBorder: brand,
    actorLineColor: tint(brand, 60),
    labelBoxBkgColor: tint(brand, FILL_PERCENT),
    labelBoxBorderColor: brand,
    activationBkgColor: tint(brand, FILL_PERCENT * 2),
    activationBorderColor: brand,
  };

  // Pie slices and every other categorical diagram (timeline, journey, git, …).
  accents.forEach((accent, index) => {
    variables[`pie${index + 1}`] = accent;
    variables[`cScale${index}`] = accent;
  });

  return variables;
}

let canvas: CanvasRenderingContext2D | null = null;

/**
 * Tokens are `oklch()`, some with alpha, which Mermaid's colour maths cannot
 * parse. Painting the colour over the page background and reading the pixel
 * back yields the opaque sRGB colour the browser would actually show.
 */
function toHex(color: string, backdrop: string): string {
  canvas ??= Object.assign(document.createElement("canvas"), { width: 1, height: 1 }).getContext(
    "2d",
    { willReadFrequently: true },
  );
  if (!canvas) return "#000000";

  canvas.clearRect(0, 0, 1, 1);
  canvas.fillStyle = backdrop;
  canvas.fillRect(0, 0, 1, 1);
  canvas.fillStyle = color;
  canvas.fillRect(0, 0, 1, 1);
  const [red = 0, green = 0, blue = 0] = canvas.getImageData(0, 0, 1, 1).data;

  return `#${[red, green, blue].map((value) => value.toString(16).padStart(2, "0")).join("")}`;
}

function readThemeVariables(element: HTMLElement): ThemeVariables {
  const style = getComputedStyle(element);
  // The node view sits on `bg-card`; translucent tokens are composited over it.
  const background = toHex(style.getPropertyValue("--card").trim(), "#ffffff");
  const dark = Number.parseInt(background.slice(1, 3), 16) < 128;
  const variables: ThemeVariables = {
    fontFamily: style.fontFamily,
    darkMode: dark,
    // Mermaid's default is a light-grey glow, which reads as a halo in dark mode.
    dropShadow: "drop-shadow(0 1px 2px rgba(0, 0, 0, 0.1))",
  };

  for (const [name, token] of Object.entries(TOKEN_VARIABLES)) {
    variables[name] = toHex(style.getPropertyValue(token).trim(), background);
  }

  const brand = style.getPropertyValue("--primary").trim();
  for (const [name, color] of Object.entries(accentVariables(brand, dark))) {
    variables[name] = toHex(color, background);
  }

  return variables;
}

let scratch: HTMLElement | null = null;

/**
 * Where Mermaid builds a diagram before handing back its SVG. Left to
 * itself it appends that scratch element to `<body>` in normal flow, so for
 * the length of a render the document is taller than the window and the
 * page flashes a scrollbar. A fixed, off-screen, invisible box is measurable
 * (Mermaid reads text sizes from it) yet adds nothing to the scrollable area.
 */
function scratchContainer(): HTMLElement {
  if (scratch?.isConnected) return scratch;
  scratch = document.createElement("div");
  scratch.setAttribute("aria-hidden", "true");
  Object.assign(scratch.style, {
    position: "fixed",
    top: "0",
    left: "-10000px",
    width: "100vw",
    visibility: "hidden",
    pointerEvents: "none",
  });
  document.body.appendChild(scratch);
  void scratch.offsetWidth;
  return scratch;
}

let queue: Promise<unknown> = Promise.resolve();
let appliedTheme = "";
let renderCount = 0;

/**
 * Renders Mermaid `source` to an SVG string themed from the shadcn tokens in
 * effect at `element`. `mermaid` is imported on first use, never at module
 * scope, so SSR and pages that never show a diagram stay clear of it.
 *
 * Renders run one at a time: `initialize` config is global to the module and
 * concurrent `render` calls interfere with each other.
 * `securityLevel: "strict"` sanitises the SVG — diagram source is document
 * content, so it may come from any collaborator or paste — and a
 * `%%{init}%%` directive in the source cannot lower it.
 */
export function renderMermaid(source: string, element: HTMLElement): Promise<string> {
  const task = queue.then(async () => {
    // Dynamic on purpose: `mermaid` is ~1 MB and touches the DOM at import,
    // so it is split out and only fetched in the browser once a diagram renders.
    const { default: mermaid } = await import("mermaid");
    const themeVariables = readThemeVariables(element);
    const theme = JSON.stringify(themeVariables);

    if (theme !== appliedTheme) {
      mermaid.initialize({
        startOnLoad: false,
        securityLevel: "strict",
        theme: "base",
        themeVariables,
        fontFamily: themeVariables.fontFamily as string,
      });
      appliedTheme = theme;
    }

    const id = `slash-mermaid-${++renderCount}`;

    try {
      return (await mermaid.render(id, source, scratchContainer())).svg;
    } finally {
      // A failed render leaves its scratch container behind in <body>.
      document.getElementById(`d${id}`)?.remove();
    }
  });

  queue = task.catch(() => undefined);
  return task;
}

function subscribeTheme(onChange: () => void): () => void {
  const observer = new MutationObserver(onChange);
  observer.observe(document.documentElement, {
    attributes: true,
    attributeFilter: ["class", "style", "data-theme"],
  });
  return () => observer.disconnect();
}

/**
 * Changes whenever the root element's theme switches (next-themes toggles a
 * `.dark` class), so a diagram can re-render in the new palette.
 */
export function useThemeSnapshot(): string {
  return useSyncExternalStore(
    subscribeTheme,
    () => `${document.documentElement.className}|${document.documentElement.style.cssText}`,
    () => "",
  );
}
