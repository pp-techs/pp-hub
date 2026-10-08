import { useQueryClient } from "@tanstack/react-query";
import { jsonEvent, useExtEvents } from "@pp-hub/ext-sdk";
import { eventsUrl, getBoard } from "./api";
import type { BoardEvent } from "./api";
import { boardKey, useApplyBoardEvent } from "./queries";

/**
 * Keeps the board live while mounted: every `board` event is applied to the
 * cached snapshot, and `reload` — or a reconnect, since events sent while the
 * stream was down are not replayed — refetches it.
 */
export function useBoardEvents(nodeId: string) {
  const qc = useQueryClient();
  const apply = useApplyBoardEvent(nodeId);

  useExtEvents({
    url: eventsUrl(nodeId),
    handlers: { board: jsonEvent<BoardEvent>(apply) },
    refetch: () =>
      qc.fetchQuery({ queryKey: boardKey(nodeId), queryFn: () => getBoard(nodeId), staleTime: 0 }),
  });
}
