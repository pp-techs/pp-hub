import { expect, test } from "vite-plus/test";
import { AppError } from "@pp-hub/app-sdk";
import { cardsIn, sortedColumns } from "../src/board-state.ts";
import { createOps } from "../src/ops.ts";
import { MAX_POSITION_LEN } from "../src/position.ts";
import { LIMITS, RuleError } from "../src/rules.ts";
import { fakeHost, noStore } from "./fake-app.ts";

function setup() {
  const host = fakeHost();
  const ops = createOps(host.app, noStore, host.board);
  return { host, ops };
}

const rule = (p: Promise<unknown>) =>
  p.then(
    () => "ok",
    (e: unknown) => (e instanceof RuleError ? e.rule : String(e)),
  );

test("columns append with the server's counter keys and refuse past 30", async () => {
  const { host, ops } = setup();
  for (const title of ["To do", "  In progress ", "Done"]) await ops.createColumn(title);
  const cols = sortedColumns(host.board());
  expect(cols.map((c) => [c.title, c.position, c.wip_limit])).toEqual([
    ["To do", "11i", null],
    ["In progress", "12i", null],
    ["Done", "13i", null],
  ]);
  expect(await rule(ops.createColumn("   "))).toBe("invalid");
  expect(await rule(ops.createColumn("x", 0))).toBe("invalid");
  while (host.board().columns.length < LIMITS.columns) await ops.createColumn("c");
  expect(await rule(ops.createColumn("one more"))).toBe("columns_full");
});

test("cards append per column; moves land between neighbours", async () => {
  const { host, ops } = setup();
  const todo = (await ops.createColumn("To do")).id;
  const done = (await ops.createColumn("Done")).id;
  const a = (await ops.createCard(todo, "a")).id;
  const b = (await ops.createCard(todo, "b")).id;
  const c = (await ops.createCard(todo, "c")).id;
  expect(cardsIn(host.board(), todo).map((x) => x.position)).toEqual(["11i", "12i", "13i"]);
  expect((await ops.createCard(done, "d")).data.position).toBe("11i");

  await ops.moveCard(c, todo, "");
  expect(cardsIn(host.board(), todo).map((x) => x.id)).toEqual([c, a, b]);
  await ops.moveCard(a, done, "");
  expect(cardsIn(host.board(), done).map((x) => x.title)).toEqual(["a", "d"]);
  expect(await rule(ops.moveCard(b, done, c))).toBe("gone");
  expect(await rule(ops.createCard("nope", "x"))).toBe("gone");
});

test("a key that grows past 48 characters renumbers the column", async () => {
  const { host, ops } = setup();
  const col = (await ops.createColumn("c")).id;
  const first = (await ops.createCard(col, "first")).id;
  await ops.createCard(col, "p");
  await ops.createCard(col, "q");
  // Keep dropping the bottom card just under the first one: each time it lands
  // between `first` and the card dropped there before, a little longer a key,
  // until the key would pass 48 characters and the column is renumbered.
  const keys = () => cardsIn(host.board(), col).map((x) => x.position);
  let rounds = 0;
  do {
    const list = cardsIn(host.board(), col);
    const moving = list.at(-1)!.id;
    await ops.moveCard(moving, col, first);
    expect(cardsIn(host.board(), col).map((x) => x.id)).toEqual([first, moving, list[1]!.id]);
    for (const k of keys()) expect(k.length).toBeLessThanOrEqual(MAX_POSITION_LEN);
    expect(++rounds).toBeLessThan(1000);
  } while (keys().join() !== "11i,12i,13i");
  expect(rounds).toBeGreaterThan(20);
});

test("deleting a column moves its cards to the end of another, or deletes them", async () => {
  const { host, ops } = setup();
  const a = (await ops.createColumn("a")).id;
  const b = (await ops.createColumn("b")).id;
  await ops.createCard(a, "a1");
  await ops.createCard(a, "a2");
  await ops.createCard(b, "b1");
  await ops.deleteColumn(a, b);
  expect(cardsIn(host.board(), b).map((x) => [x.title, x.position])).toEqual([
    ["b1", "11i"],
    ["a1", "12i"],
    ["a2", "13i"],
  ]);
  expect(host.board().columns.map((c) => c.id)).toEqual([b]);

  await ops.deleteColumn(b);
  expect(host.records().size).toBe(0);
});

test("labels: unique names (case-sensitive), #rrggbb, at most 50; deleting strips cards", async () => {
  const { host, ops } = setup();
  const col = (await ops.createColumn("c")).id;
  const bug = await ops.createLabel(" Bug ", "#ef4444");
  expect(bug.name).toBe("Bug");
  expect(await rule(ops.createLabel("Bug", "#000000"))).toBe("label_exists");
  expect((await ops.createLabel("bug", "#000000")).name).toBe("bug");
  expect(await rule(ops.createLabel("x", "red"))).toBe("invalid");
  const idea = await ops.createLabel("Idea", "#3b82f6");
  expect(await rule(ops.updateLabel(idea.id, { name: "Bug" }))).toBe("label_exists");
  await ops.updateLabel(idea.id, { name: "Idea", color: "#22c55e" });

  const card = await ops.createCard(col, "t");
  await ops.updateCard(host.board().cards[0]!, { label_ids: [bug.id, idea.id] });
  await ops.deleteLabel(bug.id);
  expect(host.records().get(card.id)?.data.label_ids).toEqual([idea.id]);

  while (host.board().labels.length < LIMITS.labels) {
    await ops.createLabel(`l${host.board().labels.length}`, "#6b7280");
  }
  expect(await rule(ops.createLabel("too many", "#6b7280"))).toBe("labels_full");
});

test("a card edit checks its fields, saves against its rev and notifies new assignees", async () => {
  const { host, ops } = setup();
  const col = (await ops.createColumn("c")).id;
  await ops.createCard(col, "Ship it");
  const card = () => host.board().cards[0]!;

  expect(await rule(ops.updateCard(card(), { title: "  " }))).toBe("invalid");
  expect(await rule(ops.updateCard(card(), { description_md: "x".repeat(20001) }))).toBe("invalid");
  expect(await rule(ops.updateCard(card(), { due_at: "2026-02-30" }))).toBe("invalid");
  expect(await rule(ops.updateCard(card(), { label_ids: ["nope"] }))).toBe("label_missing");
  expect(await rule(ops.updateCard(card(), { link_ids: ["board"] }))).toBe("link_unavailable");
  expect(await rule(ops.updateCard(card(), { link_ids: ["secret"] }))).toBe("link_unavailable");
  const many = Array.from({ length: 21 }, (_, i) => `n${i}`);
  expect(await rule(ops.updateCard(card(), { link_ids: many }))).toBe("links_full");

  host.linkable.push({ id: "doc", space_id: "space", name: "Doc", kind: "file" });
  await ops.updateCard(card(), {
    title: " Ship it now ",
    due_at: "2026-03-04T00:00:00Z",
    assignees: ["ann", "me", "ann"],
    link_ids: ["doc"],
  });
  expect(card()).toMatchObject({
    title: "Ship it now",
    due_at: "2026-03-04T00:00:00Z",
    assignees: ["ann", "me"],
    link_ids: ["doc"],
  });
  // The actor is not notified, and someone already assigned is not notified again.
  expect(host.notified).toEqual([
    { to: "ann", verb: "assigned", excerpt: "Ship it now", subject: `record:${card().id}` },
  ]);
  await ops.updateCard(card(), { assignees: ["ann", "bo"] });
  expect(host.notified.map((n) => n.to)).toEqual(["ann", "bo"]);

  // A stale rev is refused with 409.
  const stale = card();
  await ops.updateCard(card(), { title: "first" });
  const err = await ops.updateCard(stale, { title: "second" }).catch((e: unknown) => e);
  expect(err instanceof AppError && err.status).toBe(409);
});

test("the board refuses its 5001st card", async () => {
  const { host, ops } = setup();
  const col = (await ops.createColumn("c")).id;
  for (let i = 0; i < LIMITS.cards; i++) {
    await host.app.records.create("cards", {
      column_id: col,
      title: "x",
      description_md: "",
      position: "1",
      due_at: null,
      assignees: [],
      label_ids: [],
      link_ids: [],
    });
  }
  expect(await rule(ops.createCard(col, "one more"))).toBe("cards_full");
});
