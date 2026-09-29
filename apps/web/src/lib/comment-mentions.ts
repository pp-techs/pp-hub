/** An `@query` being typed: where its `@` sits, and what follows it so far. */
export interface MentionQuery {
  start: number;
  query: string;
}

/** Someone picked from the suggestions, and the label inserted for them. */
export interface PickedMention {
  id: string;
  label: string;
}

/**
 * The mention being typed right before `caret`, if any: an `@` at the start
 * of the text or after whitespace, followed by non-space characters. An `@`
 * inside a word (an email address) is not a mention.
 */
export function mentionQueryAt(text: string, caret: number): MentionQuery | null {
  const before = text.slice(0, caret);
  const match = /(?:^|\s)@([^\s@]{0,40})$/.exec(before);
  if (!match) return null;
  const query = match[1] ?? "";
  return { start: before.length - query.length - 1, query };
}

/**
 * The users a comment mentions: those picked while writing whose `@label`
 * is still in the text. Deleting the label un-mentions them, so nobody is
 * notified about a mention the author took back.
 */
export function mentionedIds(body: string, picked: readonly PickedMention[]): string[] {
  const ids = new Set<string>();
  for (const mention of picked) {
    if (body.includes(`@${mention.label}`)) ids.add(mention.id);
  }
  return [...ids];
}
