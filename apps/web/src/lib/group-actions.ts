/**
 * Splits an action list into runs of equal `group`, in order. Menus render one
 * `*MenuGroup` per run with a separator between runs, so the grouping lives in
 * the action definitions rather than in each menu.
 */
export function groupActions<T extends { group: string }>(actions: readonly T[]): T[][] {
  const runs: T[][] = [];
  for (const action of actions) {
    const last = runs.at(-1);
    if (last && last[0]!.group === action.group) last.push(action);
    else runs.push([action]);
  }
  return runs;
}
