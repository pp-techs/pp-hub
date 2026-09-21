// Space-related hooks.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { QueryClient } from "@tanstack/react-query";
import { createSpace, getSpaces, type SpaceSettings } from "@/lib/api";
import { keys } from "./keys";

export function useSpaces() {
  return useQuery({
    queryKey: keys.spaces,
    queryFn: getSpaces,
  });
}

/**
 * The one space a surface is about, from the list the rail already holds.
 *
 * `undefined` is accepted because the settings dialog asks before a space has
 * ever been opened, and "no active space" is an ordinary answer there rather
 * than a caller bug.
 */
export function useSpace(spaceId: string | undefined) {
  const spaces = useSpaces();
  return spaceId ? spaces.data?.find((s) => s.id === spaceId) : undefined;
}

/** Route-loader form: the spaces list through the shared query cache. */
export function fetchSpaces(qc: QueryClient) {
  return qc.ensureQueryData({ queryKey: keys.spaces, queryFn: getSpaces });
}

export function useCreateSpace() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      name,
      ...settings
    }: { name: string } & Omit<SpaceSettings, "name" | "trash_retention_days">) =>
      createSpace(name, settings),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.spaces }),
  });
}
