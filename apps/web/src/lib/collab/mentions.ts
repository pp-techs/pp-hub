import type { MentionItem, MentionOptions } from "@slash-editor/core";
import type { QueryClient } from "@tanstack/react-query";
import { isMarkdownFile, searchNodes } from "@/lib/api";
import { getSpaceMembers } from "@/lib/api/users";
import { keys } from "@/lib/queries/keys";
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
 * The `@` provider for a document in `spaceId`: the space's members, matched
 * on display name, name and email, then its markdown documents whose name
 * matches (from search, once the query is long enough for it). Ids are what
 * the markdown keeps — `user:<uuid>` and `doc:<uuid>` — and the member list
 * is the same cached query the rest of the app reads.
 */
export function mentionItems(queryClient: QueryClient, spaceId: string): MentionOptions["items"] {
  return async (query) => {
    const needle = query.trim().toLocaleLowerCase();
    const [members, hits] = await Promise.all([
      queryClient.fetchQuery({
        queryKey: keys.members(spaceId),
        queryFn: () => getSpaceMembers(spaceId),
        staleTime: 60_000,
      }),
      needle.length >= MIN_QUERY_LENGTH
        ? // eslint-disable-next-line lingui/no-unlocalized-strings -- search filter value
          searchNodes(needle, { spaceId, kind: "file", limit: 25 })
        : Promise.resolve([]),
    ]);

    const people: MentionItem[] = members
      .filter((m) =>
        [m.display_name, m.name, m.email].some((v) => v?.toLocaleLowerCase().includes(needle)),
      )
      .slice(0, PER_KIND)
      .map((m) => ({
        id: `${MENTION_USER}${m.user_id}`,
        label: m.display_name || m.name || m.email,
        description: m.email,
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

    return [...people, ...documents];
  };
}
