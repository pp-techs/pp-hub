// @pp-hub/ext-sdk — the web SDK of pp-hub extensions. An extension's web
// package imports this, `react`, `@tanstack/react-query`, the Lingui
// packages, `lucide-react`, `sonner` and `@workspace/ui/*`, and nothing from
// the app (docs/architecture/extensions.md).

export { defineExtension } from "./types";
export type {
  Caps,
  ExtEditorProps,
  ExtManifest,
  ExtNodeKind,
  IconComponent,
  NodeRef,
  NodeSearchHit,
  Person,
} from "./types";

export { ApiError, installExtHost } from "./host";
export type {
  ConfirmDialogProps,
  EventStreamOptions,
  ExtApi,
  ExtApiOptions,
  ExtHost,
  LoadingStatusProps,
  NodeIconProps,
  NodeSearchResult,
  ThreadListProps,
  UserAvatarProps,
} from "./host";

export {
  api,
  ConfirmDialog,
  connectEventStream,
  createModal,
  LoadingStatus,
  NodeIcon,
  ThreadList,
  useDebouncedValue,
  useKeyedState,
  useLocale,
  useNode,
  useNodeSearch,
  UserAvatar,
} from "./bridge";

export { extQueryKey, extRetry, isAppOff, jsonEvent, useExtEvents } from "./ext-query";
export type { ExtEventsOptions } from "./ext-query";
export { ExtUnavailable } from "./ext-unavailable";
