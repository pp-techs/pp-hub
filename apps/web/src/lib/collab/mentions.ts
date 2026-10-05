import type { MentionItem, MentionOptions } from "@slash-editor/core";
import type { QueryClient } from "@tanstack/react-query";
import { isMarkdownFile, searchNodes } from "@/lib/api";
import { nodePeopleOptions } from "@/lib/queries/nodes";
import { fetchSpaces } from "@/lib/queries/spaces";
import { MIN_QUERY_LENGTH } from "@/lib/queries/search";

/** How many of each kind the `@` menu lists. */
const PER_KIND = 6;

/* eslint-disable lingui/no-unlocalized-strings -- mention id prefixes stored in the markdown */
/** A mention's id names what it points at: `user:<uuid>` or `doc:<uuid>`. */
export const MENTION_USER = "user:";
export const MENTION_DOC = "doc:";
const MARKDOWN_EXT = /\.md$/i;
/* eslint-enable lingui/no-unlocalized-strings */

/**
 * The `@` provider for document `nodeId` in `spaceId`: the people who can be
 * mentioned on it (`GET /nodes/{id}/people` — the space's members for a
 * member, plus everyone holding a grant on it), matched on name and email,
 * then the space's markdown documents whose name matches (from search, once
 * the query is long enough). Documents are offered only to a member of the
 * space: search is member-scoped. Ids are what the markdown keeps —
 * `user:<uuid>` and `doc:<uuid>`.
 */
export function mentionItems(
  queryClient: QueryClient,
  nodeId: string,
  spaceId: string,
): MentionOptions["items"] {
  return async (query) => {
    const needle = query.trim().toLocaleLowerCase();
    const [people, hits] = await Promise.all([
      queryClient.fetchQuery(nodePeopleOptions(nodeId)),
      needle.length >= MIN_QUERY_LENGTH
        ? fetchSpaces(queryClient).then((spaces) =>
            spaces.some((s) => s.id === spaceId)
              ? // eslint-disable-next-line lingui/no-unlocalized-strings -- search filter value
                searchNodes(needle, { spaceId, kind: "file", limit: 25 })
              : [],
          )
        : Promise.resolve([]),
    ]);

    const candidates: MentionItem[] = people
      .filter((p) => [p.name, p.email].some((v) => v?.toLocaleLowerCase().includes(needle)))
      .slice(0, PER_KIND)
      .map((p) => ({
        id: `${MENTION_USER}${p.user_id}`,
        label: p.name || p.email,
        description: p.email,
        // eslint-disable-next-line lingui/no-unlocalized-strings -- icon key
        icon: "user",
      }));

    // Search also answers content matches; a mention names a document, so
    // only a document whose name matches is offered.
    const documents: MentionItem[] = hits
      .filter((node) => isMarkdownFile(node) && node.name.toLocaleLowerCase().includes(needle))
      .slice(0, PER_KIND)
      .map((node) => ({
        id: `${MENTION_DOC}${node.id}`,
        label: node.name.replace(MARKDOWN_EXT, ""),
        description: node.parent_name || node.space_name,
        // eslint-disable-next-line lingui/no-unlocalized-strings -- icon key
        icon: "file-text",
      }));

    return [...candidates, ...documents];
  };
}
