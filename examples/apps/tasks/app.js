// Tasks: a zero-build pp-hub packaged app. Each task is a record in the "tasks" collection,
// `{ text, done, assignee? }`; comments live in the host, attached to the subject "record:<id>".
import { connect } from "./app-sdk.js";

/** @typedef {import("./app-sdk.d.ts").AppRecord} AppRecord */

const COLLECTION = "tasks";

const VI = {
  "Add a task": "Thêm việc",
  Add: "Thêm",
  "No tasks yet.": "Chưa có việc nào.",
  "{done} of {total} done": "Đã xong {done}/{total}",
  Assign: "Giao việc",
  "Search people": "Tìm người",
  Unassign: "Bỏ giao",
  Cancel: "Hủy",
  Comments: "Bình luận",
  Delete: "Xóa",
  "Delete task?": "Xóa việc này?",
  "Someone else changed this task; showing the latest.":
    "Người khác vừa sửa việc này; đang hiển thị bản mới nhất.",
};

const app = await connect();
const { caps, user, locale, subject } = app.context;
const canEdit = caps.edit;
/** @param {string} s */
const tr = (s) => (locale.startsWith("vi") ? (VI[s] ?? s) : s);
const fail = (err) => void app.ui.toast(err.message, "error");

const $ = (id) => document.getElementById(id);
const list = $("list");
const form = $("add");
const dialog = $("assign");
const query = $("assign-query");
const results = $("assign-results");

/** @type {Map<string, AppRecord>} */
const tasks = new Map();
/** @type {Map<string, string>} user id → display name */
const names = new Map([[user.id, user.name]]);
/** @type {Record<string, number>} */
let counts = {};
let focused = subject?.startsWith("record:") ? subject.slice("record:".length) : null;

document.documentElement.lang = locale;
form.elements.text.placeholder = tr("Add a task");
form.querySelector("button").textContent = tr("Add");
form.hidden = !canEdit;
$("empty").textContent = tr("No tasks yet.");
query.placeholder = tr("Search people");
$("unassign").textContent = tr("Unassign");
$("assign-cancel").textContent = tr("Cancel");

async function refetch() {
  const [records, c] = await Promise.all([app.records.list(COLLECTION), app.comments.counts()]);
  tasks.clear();
  for (const r of records) tasks.set(r.id, r);
  counts = c;
  await resolveNames();
  render();
}

/** Looks up the names of assignees not seen yet. */
async function resolveNames() {
  const ids = [...tasks.values()].map((r) => r.data.assignee).filter((id) => id && !names.has(id));
  if (!ids.length) return;
  for (const p of await app.users.get([...new Set(ids)])) names.set(p.id, p.name);
}

function render() {
  const all = [...tasks.values()].sort((a, b) => a.created_at.localeCompare(b.created_at));
  list.replaceChildren(...all.map(row));
  const done = all.filter((r) => r.data.done).length;
  $("summary").textContent = all.length
    ? tr("{done} of {total} done").replace("{done}", done).replace("{total}", all.length)
    : "";
  $("empty").hidden = all.length > 0;
  if (focused && tasks.has(focused)) {
    list.querySelector(`[data-id="${focused}"]`)?.scrollIntoView({ block: "center" });
    focused = null;
  }
}

/**
 * Merges a patch into a task; a concurrent edit (409) reloads instead of overwriting it.
 * @param {AppRecord} r
 * @param {object} patch
 */
async function save(r, patch) {
  try {
    const next = await app.records.merge(r.id, patch, { ifRev: r.rev });
    tasks.set(next.id, next);
    render();
    return next;
  } catch (err) {
    if (err.status === 409)
      void app.ui.toast(tr("Someone else changed this task; showing the latest."));
    else fail(err);
    await refetch();
    return null;
  }
}

/** @param {string} tag @param {Record<string, unknown>} props */
function el(tag, props = {}, ...children) {
  const node = Object.assign(document.createElement(tag), props);
  node.append(...children);
  return node;
}

/** @param {AppRecord} r */
function row(r) {
  const { text = "", done = false, assignee } = r.data;
  const li = el("li", { className: done ? "done" : "" });
  li.dataset.id = r.id;
  if (subject === `record:${r.id}`) li.classList.add("focused");

  const tick = el("input", { type: "checkbox", checked: done, disabled: !canEdit });
  tick.addEventListener("change", () => save(r, { done: tick.checked }));
  const input = el("input", { className: "text", value: text, readOnly: !canEdit });
  input.addEventListener("change", () => {
    const value = input.value.trim();
    if (value && value !== text) void save(r, { text: value });
    else input.value = text;
  });

  const who = el("button", {
    className: assignee ? "chip" : "chip empty",
    textContent: assignee ? (names.get(assignee) ?? "…") : tr("Assign"),
    disabled: !canEdit,
  });
  who.addEventListener("click", () => openAssign(r));

  const n = counts[`record:${r.id}`] ?? 0;
  const comments = el("button", { textContent: n ? `${tr("Comments")} (${n})` : tr("Comments") });
  comments.addEventListener("click", () => app.comments.open(`record:${r.id}`).catch(fail));

  li.append(tick, input, who, comments);
  if (canEdit) {
    const del = el("button", { className: "danger", textContent: tr("Delete") });
    del.addEventListener("click", async () => {
      const ok = await app.ui.confirm({
        title: tr("Delete task?"),
        message: String(text),
        confirmLabel: tr("Delete"),
        destructive: true,
      });
      if (ok) app.records.delete(r.id).catch(fail);
    });
    li.append(del);
  }
  return li;
}

/** The task the assign dialog is open for. @type {AppRecord | null} */
let assigning = null;
let searchSeq = 0;

/** @param {AppRecord} r */
function openAssign(r) {
  assigning = r;
  query.value = "";
  results.replaceChildren();
  $("unassign").hidden = !r.data.assignee;
  dialog.showModal();
  void search();
}

async function search() {
  const seq = ++searchSeq;
  const people = await app.people.search(query.value.trim(), 8).catch((err) => (fail(err), []));
  if (seq !== searchSeq) return; // a newer search is on its way
  results.replaceChildren(
    ...people.map((p) => {
      names.set(p.id, p.name);
      const b = el("button", { type: "button", textContent: p.name, title: p.email ?? "" });
      b.addEventListener("click", () => assign(p.id));
      return el("li", {}, b);
    }),
  );
}

/** @param {string | null} id */
async function assign(id) {
  const r = assigning;
  dialog.close();
  if (!r || (r.data.assignee ?? null) === id) return;
  const next = await save(r, { assignee: id });
  if (next && id && id !== user.id) {
    app
      .notify({
        to: id,
        verb: "assigned",
        excerpt: String(next.data.text),
        subject: `record:${r.id}`,
      })
      .catch(fail);
  }
}

query.addEventListener("input", search);
dialog.addEventListener("close", () => {
  if (dialog.returnValue === "unassign") void assign(null);
  dialog.returnValue = "";
});

form.addEventListener("submit", async (e) => {
  e.preventDefault();
  const text = form.elements.text.value.trim();
  if (!text) return;
  try {
    const r = await app.records.create(COLLECTION, { text, done: false });
    tasks.set(r.id, r);
    form.reset();
    render();
  } catch (err) {
    fail(err);
  }
});

// Live updates from every viewer; refetch after the event stream reconnects.
app.on("records", async (e) => {
  if (e.type === "record.upsert" && e.record.collection === COLLECTION) {
    tasks.set(e.record.id, e.record);
    await resolveNames().catch(() => {});
  } else if (e.type === "record.delete") tasks.delete(e.id);
  render();
});
app.on("stream", (e) => {
  if (e.state === "reconnected") refetch().catch(fail);
});
// Comments are added in the host panel; refresh the counts when the user comes back.
window.addEventListener("focus", () => {
  app.comments
    .counts()
    .then((c) => {
      counts = c;
      render();
    })
    .catch(() => {});
});

await refetch();
