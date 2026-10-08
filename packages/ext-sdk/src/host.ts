// The seam between an extension and the app that hosts it. An extension imports
// only the SDK; the app implements `ExtHost` from its own modules and installs
// it once at startup, before any extension renders (`apps/web/src/ext/host.ts`).
// The SDK's exports (`bridge.tsx`) delegate here, so extension code never
// imports the app and a different host — a test, a future shell — can stand in.

import type { ComponentType, ReactNode } from "react";
import type { NodeRef, NodeSearchHit } from "./types";

/** Options of an `api` call: the part of ky's options extensions use. */
export interface ExtApiOptions {
  json?: unknown;
  searchParams?: Record<string, string | number | boolean | undefined>;
}

/**
 * The app's HTTP client. A request URL is relative, without a leading slash
 * (`api/v1/ext/kanban/boards/…`); a failed request resolves to a non-ok
 * `Response` rather than throwing, except a 401, which the app turns into the
 * sign-in screen.
 */
export interface ExtApi {
  get(url: string, options?: ExtApiOptions): Promise<Response>;
  post(url: string, options?: ExtApiOptions): Promise<Response>;
  put(url: string, options?: ExtApiOptions): Promise<Response>;
  patch(url: string, options?: ExtApiOptions): Promise<Response>;
  delete(url: string, options?: ExtApiOptions): Promise<Response>;
}

/** A request that came back with an error status; `message` is the server's sentence. */
export interface ApiError extends Error {
  readonly status: number;
}

export type ApiErrorConstructor = new (status: number, message: string) => ApiError;

export interface EventStreamOptions {
  /** Named events → handler of the event's `data`. */
  handlers: Record<string, (data: string) => void>;
  /**
   * Called on every successful open. `reconnected` is true from the second
   * one on: events sent while the stream was down are gone, so the caller
   * refetches whatever they would have updated.
   */
  onOpen?: (reconnected: boolean) => void;
  /**
   * Asked before each reconnect: resolving `false` stops for good, rejecting
   * counts as one more failed attempt. Without it every drop reconnects.
   */
  canReconnect?: () => Promise<boolean>;
}

export interface NodeSearchResult {
  data: NodeSearchHit[] | undefined;
  isFetching: boolean;
  /** The query is too short for the server to search; nothing was asked. */
  tooShort: boolean;
}

export interface ConfirmDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Already-translated text. */
  title: string;
  description: string;
  confirmLabel?: string;
  confirmVariant?: "destructive" | "default";
  onConfirm: () => void;
  /** A consequence the description cannot carry, set apart in the destructive colour. */
  warning?: string;
}

export interface LoadingStatusProps {
  children: ReactNode;
  className?: string;
}

export interface NodeIconProps {
  node: NodeRef;
  className?: string;
  isShared?: boolean;
}

export interface UserAvatarProps {
  src?: string;
  name?: string;
  email?: string;
  size?: "sm" | "md" | "lg";
}

export interface ThreadListProps {
  nodeId: string;
  /** The part of the node, e.g. `card:<uuid>`; threads are listed and created under it. */
  subject: string;
  canComment: boolean;
  canResolve: boolean;
  focusThreadId?: string;
  className?: string;
}

/** Everything an extension may take from the app. */
export interface ExtHost {
  api: ExtApi;
  ApiError: ApiErrorConstructor;
  connectEventStream(url: string, options: EventStreamOptions): () => void;

  ConfirmDialog: ComponentType<ConfirmDialogProps>;
  LoadingStatus: ComponentType<LoadingStatusProps>;
  NodeIcon: ComponentType<NodeIconProps>;
  UserAvatar: ComponentType<UserAvatarProps>;
  /** The per-subject discussion of a node. */
  ThreadList: ComponentType<ThreadListProps>;

  /** The active UI locale, a BCP 47 tag. */
  useLocale(): string;
  /** State that resets itself when `key` changes (modals stay mounted). */
  useKeyedState<K, V>(key: K, initial: V): [V, (next: V | ((prev: V) => V)) => void];
  /** `value`, trailing by `delay` ms. */
  useDebouncedValue<T>(value: T, delay?: number): T;
  /** A single node by id. */
  useNode(nodeId: string | undefined): { data: NodeRef | undefined };
  /** Name search for an already-debounced query. */
  useNodeSearch(query: string, options?: { spaceId?: string }): NodeSearchResult;
}

let installed: ExtHost | undefined;

/**
 * Hands the SDK the app's implementation. Call once, at startup, before an
 * extension renders.
 */
export function installExtHost(host: ExtHost): void {
  installed = host;
  ApiError = host.ApiError;
}

/** The installed host; throws when the app forgot to install one. */
export function extHost(): ExtHost {
  if (!installed) {
    // eslint-disable-next-line lingui/no-unlocalized-strings -- a wiring error for developers, never shown
    throw new Error("@pp-hub/ext-sdk: no host installed; call installExtHost() at startup");
  }
  return installed;
}

/**
 * The app's `ApiError`, bound when the host is installed so `instanceof` and
 * `new` in extension code are the app's own class. A live binding: use it only
 * after `installExtHost`.
 */
export let ApiError: ApiErrorConstructor;
