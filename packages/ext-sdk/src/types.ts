import type { ComponentType, FunctionComponent, LazyExoticComponent, SVGProps } from "react";
import type { MessageDescriptor } from "@lingui/core";
import type { LucideIcon } from "lucide-react";

/** A glyph component, as the app's file icons are: takes `className` and SVG props. */
export type IconComponent = FunctionComponent<SVGProps<SVGSVGElement>>;

/** What the signed-in user may do to a node, resolved by the server. */
export interface Caps {
  view: boolean;
  download: boolean;
  share: boolean;
  edit: boolean;
  owner: boolean;
}

/** A person as an extension lists one: an assignee, a mention. */
export interface Person {
  id: string;
  name: string;
  email?: string;
}

/** A drive node as an extension sees one: enough to draw its icon and name. */
export interface NodeRef {
  id: string;
  space_id: string;
  parent_id?: string;
  /** `ext` is a node an extension owns. */
  kind: "dir" | "file" | "ext";
  /** The registered kind of an `ext` node, e.g. `kanban.board`. */
  ext_kind?: string;
  name: string;
  caps?: Caps;
}

/** A name-search hit: a node plus where it lives. */
export interface NodeSearchHit extends NodeRef {
  /** The folder holding the hit; empty at the top level of a space. */
  parent_name: string;
}

/** The props an extension's full-page editor receives from `/edit/$nodeId`. */
export interface ExtEditorProps {
  nodeId: string;
  /**
   * The part of the node a link points at (`?subject=card:<id>`, from a
   * notification): the editor opens it.
   */
  subject?: string;
  /** A comment thread to focus (`?thread=<id>`, from a comment notification). */
  thread?: string;
}

/**
 * One kind of node an extension owns (docs/architecture/extensions.md). The
 * `kind` strings are the ones its `pp-app.json` declares.
 */
export interface ExtNodeKind {
  /** `"<ext id>.<name>"`, the value of `Node.ext_kind`. */
  kind: string;
  /** What the node is called in the UI: "Kanban board". */
  label: MessageDescriptor;
  icon: LucideIcon | IconComponent;
  /** The New-menu entry: "New kanban board". */
  newLabel: MessageDescriptor;
  /** The name a new node starts with, selected in the naming dialog. */
  defaultName: MessageDescriptor;
  /** A line under the name field saying what a new node starts with ("Columns … are added for you."). */
  newHint?: MessageDescriptor;
  /**
   * The full-page editor, opened by `/edit/$nodeId`. It fills the space under
   * the editor header and scrolls on its own; the header, the share and
   * notification controls belong to the route.
   */
  editor: LazyExoticComponent<ComponentType<ExtEditorProps>>;
}

/** The web half of an extension: what the SPA needs to show its nodes. */
export interface ExtManifest {
  /** The extension's id on the server: `/api/v1/ext/{id}`. */
  id: string;
  name: MessageDescriptor;
  nodeKinds: ExtNodeKind[];
  /**
   * How the notification bell words this extension's notifications, by the
   * verb of their kind (`ext:<id>:<verb>`). A verb with no entry is worded
   * generically.
   */
  notifications?: Record<string, (p: { actor: string; excerpt: string }) => MessageDescriptor>;
}

/** Declares an extension's web manifest; an identity function that types it. */
export function defineExtension(manifest: ExtManifest): ExtManifest {
  return manifest;
}
