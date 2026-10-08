// The SDK's delegating exports: each one forwards to the installed `ExtHost`
// (host.ts), so extension code imports these and never the app.

import type {
  ConfirmDialogProps,
  EventStreamOptions,
  ExtApi,
  LoadingStatusProps,
  NodeIconProps,
  NodeSearchResult,
  ThreadListProps,
  UserAvatarProps,
} from "./host";
import { extHost } from "./host";
import type { NodeRef } from "./types";

export { createModal } from "@buiducnhat/better-modal";

export const api: ExtApi = {
  get: (url, options) => extHost().api.get(url, options),
  post: (url, options) => extHost().api.post(url, options),
  put: (url, options) => extHost().api.put(url, options),
  patch: (url, options) => extHost().api.patch(url, options),
  delete: (url, options) => extHost().api.delete(url, options),
};

/** Opens a server-sent-events stream that reconnects on its own; returns the function that closes it. */
export function connectEventStream(url: string, options: EventStreamOptions): () => void {
  return extHost().connectEventStream(url, options);
}

export function ConfirmDialog(props: ConfirmDialogProps) {
  const Component = extHost().ConfirmDialog;
  return <Component {...props} />;
}

export function LoadingStatus(props: LoadingStatusProps) {
  const Component = extHost().LoadingStatus;
  return <Component {...props} />;
}

export function NodeIcon(props: NodeIconProps) {
  const Component = extHost().NodeIcon;
  return <Component {...props} />;
}

export function UserAvatar(props: UserAvatarProps) {
  const Component = extHost().UserAvatar;
  return <Component {...props} />;
}

export function ThreadList(props: ThreadListProps) {
  const Component = extHost().ThreadList;
  return <Component {...props} />;
}

export function useLocale(): string {
  return extHost().useLocale();
}

export function useKeyedState<K, V>(key: K, initial: V): [V, (next: V | ((prev: V) => V)) => void] {
  return extHost().useKeyedState(key, initial);
}

export function useDebouncedValue<T>(value: T, delay?: number): T {
  return extHost().useDebouncedValue(value, delay);
}

export function useNode(nodeId: string | undefined): { data: NodeRef | undefined } {
  return extHost().useNode(nodeId);
}

export function useNodeSearch(query: string, options?: { spaceId?: string }): NodeSearchResult {
  return extHost().useNodeSearch(query, options);
}
