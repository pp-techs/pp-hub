// The app's side of the extension SDK: `ExtHost` implemented from the app's own
// modules. `main.tsx` installs it before the first render, so an extension's
// `@pp-hub/ext-sdk` imports resolve to these. Typing the constant as `ExtHost`
// is what proves the app still satisfies the contract.

import type { ExtHost } from "@pp-hub/ext-sdk";
import { ThreadList } from "@/components/comments/thread-list";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { useLocale } from "@/components/locale-provider";
import { LoadingStatus } from "@/components/loading";
import { NodeIcon } from "@/components/node-icon";
import { UserAvatar } from "@/components/user-avatar";
import { api, ApiError } from "@/lib/api";
import { connectEventStream } from "@/lib/event-stream";
import { MIN_QUERY_LENGTH, useNode, useNodeSearch } from "@/lib/queries";
import { useDebounced } from "@/lib/use-debounced";
import { useKeyedState } from "@/lib/use-keyed-state";

export const appExtHost: ExtHost = {
  api,
  ApiError,
  connectEventStream,
  ConfirmDialog,
  LoadingStatus,
  NodeIcon,
  UserAvatar,
  ThreadList,
  useLocale,
  useKeyedState,
  useDebouncedValue: useDebounced,
  useNode,
  useNodeSearch(query, options) {
    const { data, isFetching } = useNodeSearch(query, options);
    return { data, isFetching, tooShort: query.trim().length < MIN_QUERY_LENGTH };
  },
};
