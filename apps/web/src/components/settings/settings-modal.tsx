import { useState } from "react";
import { createModal } from "@buiducnhat/better-modal";
import { Trans, useLingui } from "@lingui/react/macro";
import { Dialog, DialogContent, DialogTitle } from "@workspace/ui/components/dialog";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "@workspace/ui/components/select";
import { cn } from "@workspace/ui/lib/utils";
import { useSpace } from "@/lib/queries";
import { useActiveSpaceId } from "@/lib/use-active-space";
import {
  DEFAULT_SETTINGS_SLUG,
  useSettingsHash,
  useSettingsNavigation,
  type SettingsSlug,
} from "@/lib/use-settings-hash";
import { PANES, type SettingsPane } from "@/components/settings/panes";
import { SettingsDirtyProvider } from "@/components/settings/settings-dirty";
import { ConfirmDialog } from "@/components/confirm-dialog";

/**
 * The one settings surface in the app.
 *
 * The dialog never decides what it shows and never closes itself: the hash
 * does both. Every exit below is a navigation, and `SettingsHashBridge` turns
 * the resulting address back into an open or closed modal. Calling
 * `SettingsModal.hide()` from anywhere would break that — the dialog would
 * vanish while the address still named a pane, and the next reload would
 * reopen it.
 */
export const SettingsModal = createModal("settings", ({ modal }) => {
  const { t, i18n } = useLingui();
  const parsed = useSettingsHash();
  const { selectSettingsPane, closeSettings } = useSettingsNavigation();
  const space = useSpace(useActiveSpaceId());
  const [dirty, setDirty] = useState(false);
  // What a discard confirmation is standing in the way of: another pane, or
  // closing the dialog outright. Only one can be pending, because the
  // confirmation is modal over both.
  const [pendingPane, setPendingPane] = useState<SettingsSlug | null>(null);
  const [pendingClose, setPendingClose] = useState(false);

  const panes = PANES.filter((p) => p.group === "account" || space?.is_admin === true);
  const active =
    panes.find((p) => p.slug === parsed?.slug) ??
    panes.find((p) => p.slug === DEFAULT_SETTINGS_SLUG);

  const groups: { label: string; panes: SettingsPane[] }[] = [
    { label: t`Account`, panes: panes.filter((p) => p.group === "account") },
  ];
  if (space?.is_admin) {
    groups.push({ label: space.name, panes: panes.filter((p) => p.group === "space") });
  }

  if (!active) return null;
  const Pane = active.Component;
  const activeGroup = groups.find((g) => g.panes.includes(active));

  const requestPane = (slug: SettingsSlug) => {
    if (slug === active.slug) return;
    if (dirty) setPendingPane(slug);
    else selectSettingsPane(slug);
  };

  const clearPending = () => {
    setPendingPane(null);
    setPendingClose(false);
  };

  const discardAndContinue = () => {
    setDirty(false);
    if (pendingClose) closeSettings();
    else if (pendingPane) selectSettingsPane(pendingPane);
    clearPending();
  };

  return (
    <Dialog
      open={modal.visible}
      onOpenChange={(open, details) => {
        if (open) return;
        // Escape and a click on the backdrop are single, easily mistaken
        // gestures, so while a pane is dirty they do nothing at all. The close
        // button is deliberate enough to be worth a question instead.
        if (dirty && details.reason !== "close-press") return;
        if (dirty) setPendingClose(true);
        else closeSettings();
      }}
    >
      {/* Wide enough for the member table, which is the one pane that is a
          table rather than a form; the rest simply cap their own column. */}
      <DialogContent className="gap-0 overflow-hidden p-0 sm:max-w-5xl">
        <DialogTitle className="sr-only">
          <Trans>Settings</Trans>
        </DialogTitle>

        <div className="flex h-[min(34rem,80vh)]">
          {/* Plain markup, not the `Sidebar` primitive: the modal container
              lives at the router root, outside the drive shell's
              `SidebarProvider`, and nesting a second one would register a
              second ⌘B handler that toggles the rail behind the dialog. */}
          <nav
            aria-label={t`Settings sections`}
            className="bg-sidebar hidden w-52 shrink-0 flex-col gap-4 overflow-y-auto border-r p-2 md:flex"
          >
            {groups.map((group) => (
              <div key={group.label} className="flex flex-col gap-0.5">
                <div className="text-muted-foreground truncate px-2 py-1 text-[10px] font-medium tracking-wide uppercase">
                  {group.label}
                </div>
                {group.panes.map((pane) => (
                  <button
                    key={pane.slug}
                    type="button"
                    aria-current={pane.slug === active.slug ? "page" : undefined}
                    onClick={() => requestPane(pane.slug)}
                    className={cn(
                      "flex items-center gap-2 rounded-md px-2 py-1.5 text-left text-xs transition-colors",
                      pane.slug === active.slug
                        ? "bg-sidebar-accent text-sidebar-accent-foreground font-medium"
                        : pane.danger
                          ? "text-destructive/70 hover:bg-sidebar-accent/50 hover:text-destructive"
                          : "text-muted-foreground hover:bg-sidebar-accent/50 hover:text-foreground",
                    )}
                  >
                    <pane.icon className="size-3.5 shrink-0" />
                    <span className="truncate">{i18n._(pane.label)}</span>
                  </button>
                ))}
              </div>
            ))}
          </nav>

          <div className="flex min-w-0 flex-1 flex-col">
            {/* `pr-12` keeps the header clear of the dialog's own close
                button, which sits at the top right of the whole popup. */}
            <header className="border-border flex h-12 shrink-0 items-center gap-2 border-b px-4 pr-12">
              <h2 className="font-heading hidden min-w-0 items-baseline gap-1.5 text-xs font-medium md:flex">
                {activeGroup && (
                  <span className="text-muted-foreground truncate font-normal">
                    {activeGroup.label}
                  </span>
                )}
                <span className="truncate">{i18n._(active.label)}</span>
              </h2>

              <Select
                value={active.slug}
                onValueChange={(v) => requestPane(v as SettingsSlug)}
                items={panes.map((p) => ({ label: i18n._(p.label), value: p.slug }))}
              >
                <SelectTrigger aria-label={t`Settings sections`} className="w-full md:hidden">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {groups.map((group) => (
                    <SelectGroup key={group.label}>
                      <SelectLabel>{group.label}</SelectLabel>
                      {group.panes.map((pane) => (
                        <SelectItem key={pane.slug} value={pane.slug}>
                          {i18n._(pane.label)}
                        </SelectItem>
                      ))}
                    </SelectGroup>
                  ))}
                </SelectContent>
              </Select>
            </header>

            <div className="min-h-0 flex-1 overflow-auto p-4">
              <SettingsDirtyProvider value={setDirty}>
                <Pane />
              </SettingsDirtyProvider>
            </div>
          </div>
        </div>

        <ConfirmDialog
          open={pendingPane !== null || pendingClose}
          onOpenChange={(open) => !open && clearPending()}
          title={t`Discard unsaved changes?`}
          description={t`This pane has changes that were never saved. Leaving now throws them away.`}
          confirmLabel={t`Discard changes`}
          onConfirm={discardAndContinue}
        />
      </DialogContent>
    </Dialog>
  );
});
