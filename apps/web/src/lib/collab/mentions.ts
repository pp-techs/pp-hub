import type { MentionItem, MentionOptions } from "@slash-editor/core";
import type { QueryClient } from "@tanstack/react-query";
import { nodePeopleOptions } from "@/lib/queries/nodes";

/** How many people the `@` menu lists. */
const LIMIT = 6;

/* eslint-disable lingui/no-unlocalized-strings -- mention id prefix stored in the markdown */
/** A mention's id names who it points at: `user:<uuid>`. */
export const MENTION_USER = "user:";
/* eslint-enable lingui/no-unlocalized-strings */

/**
 * The `@` provider for document `nodeId`: the people who can be mentioned on
 * it (`GET /nodes/{id}/people` — the space's members for a member, plus
 * everyone holding a grant on it), matched on name and email. Ids are what
 * the markdown keeps — `user:<uuid>`. Pages are not offered here: the editor
 * merges its page store's search into the menu (`withPageMentions`), and a
 * pick inserts a `pageLink`.
 */
export function mentionItems(queryClient: QueryClient, nodeId: string): MentionOptions["items"] {
  return async (query) => {
    const needle = query.trim().toLocaleLowerCase();
    const people = await queryClient.fetchQuery(nodePeopleOptions(nodeId));
    return people
      .filter((p) => [p.name, p.email].some((v) => v?.toLocaleLowerCase().includes(needle)))
      .slice(0, LIMIT)
      .map((p): MentionItem => ({
        id: `${MENTION_USER}${p.user_id}`,
        label: p.name || p.email,
        description: p.email,
        // eslint-disable-next-line lingui/no-unlocalized-strings -- icon key
        icon: "user",
      }));
  };
}
