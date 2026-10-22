import { expect, test } from "vite-plus/test";
import { after, between, spread } from "../src/position.ts";
// Produced by the Go `position.go` the server used (same LCG as below), so these
// pin the port byte for byte.
import golden from "./position-golden.json";

/** The LCG the golden file was generated with. */
function lcg(seed: number) {
  let x = seed;
  return (n: number) => {
    x = (Math.imul(x, 1103515245) + 12345) & 0x7fffffff;
    return x % n;
  };
}

const sorted = (keys: string[]) => keys.every((k, i) => i === 0 || keys[i - 1]! <= k);

test("between matches the Go implementation on every insert of the golden run", () => {
  const next = lcg(1);
  const keys = [between("", "")];
  for (const [a, b, want] of golden.between) {
    const at = next(keys.length + 1);
    expect([keys[at - 1] ?? "", keys[at] ?? ""]).toEqual([a, b]);
    expect(between(a!, b!)).toBe(want);
    keys.splice(at, 0, want!);
  }
});

test("after and spread match the Go implementation", () => {
  let last = "";
  const want = golden.after as Record<string, string>;
  for (let i = 1; i <= 50000; i++) {
    last = after(last);
    if (want[i]) expect(last).toBe(want[i]);
  }
  for (const [from, key] of golden.afterHand) expect(after(from!)).toBe(key);
  expect(spread(40)).toEqual(golden.spread);
});

// The cases of position_test.go.

test("between orders and never ends in zero", () => {
  // Insert at random places into a growing list and check the order the keys
  // give is the order the inserts asked for.
  const next = lcg(7);
  const keys = [between("", "")];
  for (let i = 0; i < 2000; i++) {
    const at = next(keys.length + 1); // insert before keys[at]
    const a = at > 0 ? keys[at - 1]! : "";
    const b = at < keys.length ? keys[at]! : "";
    const k = between(a, b);
    if ((a !== "" && !(a < k)) || (b !== "" && !(k < b))) {
      throw new Error(`between(${a}, ${b}) = ${k} is not between`);
    }
    expect(k.endsWith("0")).toBe(false);
    keys.splice(at, 0, k);
  }
  expect(sorted(keys)).toBe(true);
});

test("after grows logarithmically", () => {
  let last = "";
  let prev = "";
  for (let i = 0; i < 5000; i++) {
    last = after(last);
    if (prev !== "") expect(prev < last).toBe(true);
    expect(last.endsWith("0")).toBe(false);
    prev = last;
  }
  expect(last.length).toBeLessThanOrEqual(5);
});

test("spread is ordered and usable by between", () => {
  const keys = spread(1500);
  expect(sorted(keys)).toBe(true);
  expect(new Set(keys).size).toBe(keys.length);
  for (const k of keys) expect(k.endsWith("0")).toBe(false);
  const mid = between(keys[10]!, keys[11]!);
  expect(keys[10]! < mid && mid < keys[11]!).toBe(true);
});

test("the seed columns are the first three appends", () => {
  expect([after(""), after(after("")), after(after(after("")))]).toEqual(spread(3));
  expect(spread(3)).toEqual(["11i", "12i", "13i"]);
});
