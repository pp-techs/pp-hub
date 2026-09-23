import type { GrantCaps, Node } from "./types";

/**
 * What the current user may do to one node, as the server resolved it.
 *
 * The client never derives this. Access comes from ownership plus grants on
 * the node or any ancestor, and inside a team folder ownership stops counting
 * — a walk the browser has neither the rows nor the right to do. Every node
 * the API hands back carries its own answer, and the UI's only job is to stop
 * offering what the answer excludes.
 */
export type NodeCaps = NonNullable<Node["caps"]>;

/**
 * The answer for a node whose caps nobody resolved: enough to look at it,
 * nothing else.
 *
 * Every authenticated node response sets caps, so this is a floor rather than
 * a fallback — it exists so an unstamped surface degrades into a read-only
 * one instead of showing controls that 403 on click. Widening it to "assume
 * allowed" would put the guess back where it started.
 */
const VIEW_ONLY: NodeCaps = {
  view: true,
  download: false,
  share: false,
  edit: false,
  owner: false,
};

export function capsOf(node: Node | null | undefined): NodeCaps {
  return node?.caps ?? VIEW_ONLY;
}

/**
 * What is allowed on *every* node of a selection.
 *
 * A bulk action runs per node and each call is authorised on its own, so the
 * bar may only offer what holds for all of them — otherwise "Delete 5 items"
 * means "delete the three I'm allowed to", which is not what it says.
 */
export function commonCaps(nodes: Node[]): NodeCaps {
  if (nodes.length === 0) return VIEW_ONLY;
  return nodes.reduce<NodeCaps>((acc, node) => {
    const c = capsOf(node);
    return {
      view: acc.view && c.view,
      download: acc.download && c.download,
      share: acc.share && c.share,
      edit: acc.edit && c.edit,
      owner: acc.owner && c.owner,
    };
  }, capsOf(nodes[0]));
}

/**
 * Whether renaming, trashing or moving this node away is the caller's to do.
 *
 * Edit is the rule everywhere except the root of a team folder, which is the
 * team's shape rather than a file in it: only a manager — a grantee whose
 * grant carries re-sharing — may rename or destroy it.
 */
export function canRestructure(node: Node): boolean {
  const c = capsOf(node);
  return node.team_folder ? c.share : c.edit;
}

/**
 * Whether `caps` holds everything `grant` hands over.
 *
 * The client-side mirror of the server's `Caps.Covers`: you can only give out
 * what you hold, and only change or revoke a grant no wider than your own
 * access — anything else is a 403. So a grant row the caller does not cover
 * carries no controls at all, and an option the caller does not hold is not
 * offered when granting.
 */
export function covers(caps: NodeCaps, grant: GrantCaps): boolean {
  return (
    (grant.level !== "edit" || caps.edit) &&
    (!grant.can_download || caps.download) &&
    (!grant.can_share || caps.share)
  );
}
