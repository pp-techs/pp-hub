import { useEffect } from "react";
import { useSpaces } from "@/lib/queries";
import { useActiveSpaceId } from "@/lib/use-active-space";
import {
  DEFAULT_SETTINGS_SLUG,
  useSettingsHash,
  useSettingsNavigation,
} from "@/lib/use-settings-hash";
import { PANES, paneAvailable } from "@/components/settings/panes";
import { SettingsModal } from "@/components/settings/settings-modal";

/**
 * Turns the address into an open or closed settings dialog, and nothing else.
 *
 * This is the whole glue between the hash and `better-modal`, and it only
 * flows one way. The dialog is shown because the address says so; it is
 * hidden because the address stopped saying so. Nothing downstream ever calls
 * `show`/`hide`, which is what keeps the URL the single source of truth for
 * which pane is open across reloads, Back, and a pasted link.
 *
 * Mounted in the drive shell rather than the root: `#settings/general` on the
 * login page or a share link is meaningless and must stay inert.
 */
export function SettingsHashBridge() {
  const parsed = useSettingsHash();
  const spaceId = useActiveSpaceId();
  const spaces = useSpaces();
  const { selectSettingsPane } = useSettingsNavigation();

  const open = parsed !== null;
  const slug = parsed?.slug ?? null;

  // Boolean dependency on purpose: switching pane must not call `show()` a
  // second time, which better-modal answers by rejecting the first promise.
  useEffect(() => {
    if (open) void SettingsModal.show().catch(() => undefined);
    else SettingsModal.hide();
  }, [open]);

  // A space pane is admin-only (and some are organization-only), and the
  // answer arrives with the spaces query. Waiting for `isSuccess` matters:
  // treating "not loaded yet" as "not an admin" would bounce a deep link to a
  // space pane before the permission it needs had even been fetched.
  const pane = PANES.find((p) => p.slug === slug);
  const forbidden =
    pane !== undefined &&
    spaces.isSuccess &&
    !paneAvailable(
      pane,
      spaces.data.find((s) => s.id === spaceId),
    );

  useEffect(() => {
    if (!open) return;
    // An unknown slug and a forbidden one get the same answer: replace the
    // address with the default pane. Nothing is said about the pane that was
    // asked for, because saying it would confirm that it exists.
    if (slug === null || forbidden) selectSettingsPane(DEFAULT_SETTINGS_SLUG);
  }, [open, slug, forbidden, selectSettingsPane]);

  return null;
}
