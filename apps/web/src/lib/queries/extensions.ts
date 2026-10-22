// Extensions: which small apps this server carries and where they are on, and
// the uploaded ones an administrator installs, updates and removes (M51).

import { queryOptions, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  createExtNode,
  getAdminApp,
  getExtensions,
  installApp,
  listExtNodes,
  removeApp,
  rotateAppSecret,
  setInstanceExtension,
  setSpaceExtension,
  updateApp,
} from "@/lib/api";
import { registerPackagedApps } from "@/ext/registry";
import { keys } from "./keys";
import { useInvalidateChildren } from "./nodes";

/**
 * The extensions list. With a space, `enabled` is the effective answer there;
 * without one it is the instance switch. A space-less call is the admin page's.
 * Every fetch hands the uploaded apps it lists to the registry: that is how
 * the SPA learns them.
 */
export function extensionsOptions(spaceId?: string) {
  return queryOptions({
    queryKey: keys.extensions(spaceId),
    queryFn: async () => {
      const list = await getExtensions(spaceId);
      registerPackagedApps(list);
      return list;
    },
  });
}

export function useExtensions(spaceId?: string, enabled = true) {
  return useQuery({ ...extensionsOptions(spaceId), enabled });
}

/** One uploaded app as Admin › Apps manages it: its backend address and secret. */
export function useAdminApp(extId: string, enabled = true) {
  return useQuery({
    queryKey: keys.adminApp(extId),
    queryFn: () => getAdminApp(extId),
    enabled,
  });
}

/** Install an uploaded package, or replace the installed version of its id. */
export function useInstallApp() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ file, upgrade, id }: { file: Blob; upgrade: boolean; id?: string }) =>
      installApp(file, upgrade, id),
    onSuccess: (app) => {
      qc.setQueryData(keys.adminApp(app.id), app);
      void qc.invalidateQueries({ queryKey: keys.extensionsRoot });
    },
  });
}

export function useUpdateApp(extId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (backendURL: string) => updateApp(extId, backendURL),
    onSuccess: (app) => {
      qc.setQueryData(keys.adminApp(extId), app);
      void qc.invalidateQueries({ queryKey: keys.extensionsRoot });
    },
  });
}

export function useRotateAppSecret(extId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => rotateAppSecret(extId),
    onSuccess: ({ app }) => qc.setQueryData(keys.adminApp(extId), app),
  });
}

export function useRemoveApp() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ extId, purge }: { extId: string; purge: boolean }) => removeApp(extId, purge),
    onSuccess: (_, { extId }) => {
      qc.removeQueries({ queryKey: keys.adminApp(extId) });
      void qc.invalidateQueries({ queryKey: keys.extensionsRoot });
    },
  });
}

/** One app's nodes in a space — the rail's Apps section lists them while the app is expanded. */
export function extNodesOptions(spaceId: string, extId: string) {
  return queryOptions({
    queryKey: keys.extNodes(spaceId, extId),
    queryFn: () => listExtNodes(spaceId, extId),
  });
}

export function useExtNodes(spaceId: string, extId: string, enabled = true) {
  return useQuery({ ...extNodesOptions(spaceId, extId), enabled });
}

/** Instance switch. Every space's answer may change, so the whole root refreshes. */
export function useSetInstanceExtension() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ extId, enabled }: { extId: string; enabled: boolean }) =>
      setInstanceExtension(extId, enabled),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: keys.extensionsRoot });
    },
  });
}

/**
 * Space switch. Listings badge a node whose app is off, so the space's
 * listings refresh with the extensions list.
 */
export function useSetSpaceExtension(spaceId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ extId, enabled }: { extId: string; enabled: boolean }) =>
      setSpaceExtension(spaceId, extId, enabled),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: keys.extensionsRoot });
      void qc.invalidateQueries({ queryKey: ["children", spaceId] });
      void qc.invalidateQueries({ queryKey: keys.extNodesRoot(spaceId) });
      void qc.invalidateQueries({ queryKey: ["node-children"] });
      void qc.invalidateQueries({ queryKey: ["recent", spaceId] });
      void qc.invalidateQueries({ queryKey: ["starred", spaceId] });
    },
  });
}

export function useCreateExtNode(spaceId: string, parentId?: string) {
  const invalidate = useInvalidateChildren(spaceId);
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ extKind, name }: { extKind: string; name: string }) =>
      createExtNode(spaceId, extKind, name, parentId),
    onSuccess: () => {
      void invalidate();
      void qc.invalidateQueries({ queryKey: keys.extensions(spaceId) });
    },
  });
}
