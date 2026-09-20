// Turning a user-agent string into something a person recognises.
//
// Shared by the account's session list and the admin security log: both
// answer "which device was this?", and two copies of the table would drift
// until the same browser was named differently on the two pages.

/** Edge and Opera also say "Chrome", and Chrome also says "Safari" — order is the test. */
const BROWSERS: [match: RegExp, name: string][] = [
  [/\bEdgi?A?\//, "Edge"],
  [/\bOPR\//, "Opera"],
  [/\bFirefox\//, "Firefox"],
  [/\bChrome\//, "Chrome"],
  [/\bSafari\//, "Safari"],
];

export type FormFactor = "desktop" | "phone" | "tablet";

const PLATFORMS: [match: RegExp, name: string, form: FormFactor][] = [
  [/\biPhone\b/, "iPhone", "phone"],
  [/\biPad\b/, "iPad", "tablet"],
  [/\bAndroid\b/, "Android", "phone"],
  [/\bCrOS\b/, "ChromeOS", "desktop"],
  [/\bMac OS X\b|\bMacintosh\b/, "macOS", "desktop"],
  [/\bWindows\b/, "Windows", "desktop"],
  [/\bLinux\b/, "Linux", "desktop"],
];

/**
 * "Chrome on macOS" out of a user-agent string.
 *
 * Deliberately crude: this line only has to let somebody recognise their own
 * laptop in a list of three, and a full UA database would be a dependency
 * carried for one sentence. Anything unrecognised — a script, a mobile app,
 * a new browser — shows its raw agent rather than a wrong guess.
 */
export function describeAgent(ua: string): { label: string; form: FormFactor } {
  const browser = BROWSERS.find(([match]) => match.test(ua))?.[1];
  const platform = PLATFORMS.find(([match]) => match.test(ua));

  if (browser && platform) return { label: `${browser} on ${platform[1]}`, form: platform[2] };
  if (platform) return { label: platform[1], form: platform[2] };
  if (browser) return { label: browser, form: "desktop" };
  return { label: ua.trim() || "Unknown device", form: "desktop" };
}
