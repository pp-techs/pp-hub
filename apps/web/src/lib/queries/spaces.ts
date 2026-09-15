// Space-related hooks.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createSpace, getSpaces, type SpaceSettings } from "@/lib/api";
import { keys } from "./keys";

export function useSpaces() {
  return useQuery({
    queryKey: keys.spaces,
    queryFn: getSpaces,
  });
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
