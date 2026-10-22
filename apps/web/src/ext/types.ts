import type { ComponentType, FunctionComponent, LazyExoticComponent, SVGProps } from "react";
import type { MessageDescriptor } from "@lingui/core";
import type { LucideIcon } from "lucide-react";

// What the SPA knows of an installed app (M51): built from `GET /extensions`
// by `packaged.tsx`, read through `registry.ts`. The SPA carries no code for
// any app; these are the shapes its own screens draw an app with.

/** A glyph component, as the drive's file icons are: takes `className` and SVG props. */
export type IconComponent = FunctionComponent<SVGProps<SVGSVGElement>>;

/** The props an app's editor receives from the file view. */
export interface ExtEditorProps {
  nodeId: string;
  /** The part of the node a link points at (`?subject=record:<id>`, from a notification). */
  subject?: string;
  /** A comment thread to focus (`?thread=<id>`, from a comment notification). */
  thread?: string;
}

/** One kind of node an app owns. */
export interface ExtNodeKind {
  /** `"<app id>.<name>"`, the value of `Node.ext_kind`. */
  kind: string;
  /** What the node is called in the UI: "Kanban board". */
  label: MessageDescriptor;
  icon: LucideIcon | IconComponent;
  /** The New-menu entry: "New kanban board". */
  newLabel: MessageDescriptor;
  /** The name a new node starts with, selected in the naming dialog. */
  defaultName: MessageDescriptor;
  /** A line under the name field saying what a new node starts with. */
  newHint?: MessageDescriptor;
  /** The editor the file view opens the node in: the app's sandboxed frame. */
  editor: LazyExoticComponent<ComponentType<ExtEditorProps>>;
}

/** An installed app, as the SPA's screens draw it. */
export interface ExtManifest {
  /** The app's id on the server: `/api/v1/apps/{id}`. */
  id: string;
  name: MessageDescriptor;
  nodeKinds: ExtNodeKind[];
  /**
   * How the notification bell words this app's notifications, by the verb of
   * their kind (`ext:<id>:<verb>`). A verb with no entry is worded generically.
   */
  notifications?: Record<string, (p: { actor: string; excerpt: string }) => MessageDescriptor>;
}
