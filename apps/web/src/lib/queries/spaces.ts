// Space-related hooks.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createSpace, getSpaces } from "@/lib/api";
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
    mutationFn: (name: string) => createSpace(name),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.spaces }),
  });
}
