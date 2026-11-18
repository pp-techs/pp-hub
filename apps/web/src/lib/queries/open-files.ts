// The files open in an editor right now, for the admin console.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { getAdminOpenFiles, saveAdminOpenFile, saveAdminOpenFiles } from "@/lib/api";
import { keys } from "./keys";

/** A save OnlyOffice queued becomes a version a moment later; the list is read again after it. */
const VERSION_SETTLE_MS = 3_000;

/**
 * Re-read on a timer: this page is looked at before a deploy, and a list that
 * was true a minute ago is the wrong thing to base one on.
 */
export function useAdminOpenFiles() {
  return useQuery({
    queryKey: keys.adminOpenFiles,
    queryFn: getAdminOpenFiles,
    refetchInterval: 15_000,
  });
}

function useRefreshOpenFiles() {
  const qc = useQueryClient();
  return () => {
    void qc.invalidateQueries({ queryKey: keys.adminOpenFiles });
    setTimeout(
      () => void qc.invalidateQueries({ queryKey: keys.adminOpenFiles }),
      VERSION_SETTLE_MS,
    );
  };
}

export function useSaveAdminOpenFiles() {
  const refresh = useRefreshOpenFiles();
  return useMutation({ mutationFn: saveAdminOpenFiles, onSuccess: refresh });
}

export function useSaveAdminOpenFile() {
  const refresh = useRefreshOpenFiles();
  return useMutation({
    mutationFn: (nodeId: string) => saveAdminOpenFile(nodeId),
    onSuccess: refresh,
  });
}
