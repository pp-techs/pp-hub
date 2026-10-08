import type { ComponentType } from "react";
import type { MessageDescriptor } from "@lingui/core";
import { msg } from "@lingui/core/macro";
import {
  Building2,
  Puzzle,
  ShieldCheck,
  SlidersHorizontal,
  TriangleAlert,
  User,
  Users,
  UsersRound,
  type LucideIcon,
} from "lucide-react";
import type { Space } from "@/lib/api";
import type { SettingsSlug } from "@/lib/use-settings-hash";
import { AccountGeneralPane } from "@/components/settings/panes/account-general";
import { AccountProfilePane } from "@/components/settings/panes/account-profile";
import { AccountSecurityPane } from "@/components/settings/panes/account-security";
import { SpaceAppsPane } from "@/components/settings/panes/space-apps";
import { SpaceDangerPane } from "@/components/settings/panes/space-danger";
import { SpaceGeneralPane } from "@/components/settings/panes/space-general";
import { SpaceGroupsPane } from "@/components/settings/panes/space-groups";
import { SpaceMembersPane } from "@/components/settings/panes/space-members";

/**
 * Which authority a pane belongs to, and therefore who may open it.
 *
 * `account` is about the person and is always available. `space` is about the
 * space the rail is pointed at and is admin-only, so the whole band disappears
 * for everybody else rather than rendering rows that refuse to work.
 */
export type SettingsGroup = "account" | "space";

export interface SettingsPane {
  slug: SettingsSlug;
  group: SettingsGroup;
  /**
   * A descriptor, never rendered text: this list is module scope and runs at
   * import time, before any catalog is active, so a string here would pin the
   * first language loaded and never follow a switch.
   */
  label: MessageDescriptor;
  icon: LucideIcon;
  /** Renders the nav row in the destructive tone, for a Danger zone. */
  danger?: boolean;
  /** Space panes that make no sense on a personal space. */
  orgOnly?: boolean;
  /**
   * Panes take no props. A space pane reads the active space the same way the
   * rail does, through `useActiveSpaceId()`, which works from the modal
   * container at the root as well as from inside a route.
   */
  Component: ComponentType;
}

/**
 * Every settings pane, in the order the dialog offers them.
 *
 * Account comes first because it is the part everyone has and the part they
 * come back to; the space band below it is a different authority, not a
 * continuation of the same list.
 */
export const PANES: readonly SettingsPane[] = [
  {
    slug: "general",
    group: "account",
    label: msg`General`,
    icon: SlidersHorizontal,
    Component: AccountGeneralPane,
  },
  {
    slug: "profile",
    group: "account",
    label: msg`Profile`,
    icon: User,
    Component: AccountProfilePane,
  },
  {
    slug: "security",
    group: "account",
    label: msg`Security`,
    icon: ShieldCheck,
    Component: AccountSecurityPane,
  },
  {
    slug: "space",
    group: "space",
    label: msg`General`,
    icon: Building2,
    Component: SpaceGeneralPane,
  },
  {
    slug: "members",
    group: "space",
    label: msg`Members`,
    icon: Users,
    Component: SpaceMembersPane,
    orgOnly: true,
  },
  {
    slug: "groups",
    group: "space",
    label: msg`Groups`,
    icon: UsersRound,
    Component: SpaceGroupsPane,
    orgOnly: true,
  },
  {
    slug: "apps",
    group: "space",
    label: msg`Apps`,
    icon: Puzzle,
    Component: SpaceAppsPane,
  },
  {
    slug: "danger",
    group: "space",
    label: msg`Danger zone`,
    icon: TriangleAlert,
    danger: true,
    orgOnly: true,
    Component: SpaceDangerPane,
  },
];

/**
 * Whether `pane` is offered for `space`: account panes always; space panes to
 * the space's admins, minus the organization-only ones on a personal space —
 * it has one member, no groups and cannot be deleted.
 */
export function paneAvailable(
  pane: SettingsPane,
  space: Pick<Space, "is_admin" | "kind"> | undefined,
): boolean {
  if (pane.group === "account") return true;
  if (space?.is_admin !== true) return false;
  return !(pane.orgOnly && space.kind === "personal");
}
