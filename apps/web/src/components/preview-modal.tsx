import { createModal } from "@buiducnhat/better-modal";
import { ChevronLeft, ChevronRight, Download, ExternalLink, X } from "lucide-react";
import { Dialog, DialogContent, DialogTitle } from "@workspace/ui/components/dialog";
import { Button } from "@workspace/ui/components/button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@workspace/ui/components/tabs";
import { canOpenInTab, fmtDate, fmtSize, openInTab, type Node } from "@/lib/api";
import { PreviewContent } from "@/components/preview-content";
import { VersionList } from "@/components/version-list";
import { NodeIcon } from "@/components/node-icon";
import { useKeyedState } from "@/lib/use-keyed-state";

type Props = {
  node: Node;
  /**
   * Files shown alongside this one, so the modal can page through them without
   * closing. Optional: a preview opened from somewhere with no listing behind
   * it simply has no pager.
   */
  siblings?: Node[];
};

export const PreviewModal = createModal<Props, void>("preview", ({ node, siblings, modal }) => {
  const files = siblings?.filter((n) => n.kind === "file") ?? [];
  const startIndex = Math.max(
    0,
    files.findIndex((n) => n.id === node.id),
  );
  // Keyed on the file it was opened for. better-modal keeps a closed modal
  // mounted, so a plain useState would reopen on whichever file was viewed
  // last — see use-keyed-state.ts.
  const [index, setIndex] = useKeyedState(node.id, startIndex);

  // Falls back to the node it was opened with when there is no listing.
  const current = files[index] ?? node;
  const canPage = files.length > 1;

  const step = (delta: number) => setIndex((i) => (i + delta + files.length) % files.length);

  return (
    <Dialog open={modal.visible} onOpenChange={(open) => !open && modal.resolve(undefined)}>
      <DialogContent
        showCloseButton={false}
        className="flex h-[85vh] max-h-[85vh] flex-col gap-0 p-0 !max-w-[90vw] sm:!max-w-[90vw]"
        onKeyDown={(e) => {
          if (!canPage) return;
          if (e.key === "ArrowRight") {
            e.preventDefault();
            step(1);
          } else if (e.key === "ArrowLeft") {
            e.preventDefault();
            step(-1);
          }
        }}
      >
        <div className="border-border flex h-12 shrink-0 items-center gap-2 border-b px-3">
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label="Close preview"
            onClick={() => modal.resolve(undefined)}
          >
            <X />
          </Button>
          <NodeIcon node={current} className="size-4 shrink-0" />
          <div className="flex min-w-0 flex-col">
            <DialogTitle className="truncate text-sm font-medium" title={current.name}>
              {current.name}
            </DialogTitle>
            <span className="text-muted-foreground truncate text-[10px] tabular-nums">
              {[
                current.size != null ? fmtSize(current.size) : null,
                current.version ? `version ${current.version}` : null,
              ]
                .filter(Boolean)
                .join(" · ")}
            </span>
          </div>

          {canPage && (
            <div className="mx-auto flex shrink-0 items-center gap-1">
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label="Previous file"
                onClick={() => step(-1)}
              >
                <ChevronLeft />
              </Button>
              <span className="text-muted-foreground text-xs tabular-nums">
                {index + 1} of {files.length}
              </span>
              <Button variant="ghost" size="icon-sm" aria-label="Next file" onClick={() => step(1)}>
                <ChevronRight />
              </Button>
            </div>
          )}

          <div className="ml-auto flex shrink-0 items-center gap-1">
            {canOpenInTab(current) && (
              <Button variant="ghost" size="sm" onClick={() => openInTab(current)}>
                <ExternalLink data-icon="inline-start" /> Open
              </Button>
            )}
            <Button
              variant="ghost"
              size="sm"
              render={<a href={`/api/v1/nodes/${current.id}/download`} />}
            >
              <Download data-icon="inline-start" /> Download
            </Button>
          </div>
        </div>

        <div className="flex min-h-0 flex-1">
          <div className="flex min-h-0 min-w-0 flex-1 flex-col overflow-auto">
            {/* Keyed so paging remounts the viewer instead of showing the
                  previous file's bytes under the new file's name. */}
            <PreviewContent key={current.id} node={current} />
          </div>

          <aside className="border-border flex w-80 shrink-0 flex-col border-l">
            <Tabs defaultValue="details" className="flex min-h-0 flex-1 flex-col">
              <TabsList className="border-border shrink-0 rounded-none border-b px-2">
                <TabsTrigger value="details">Details</TabsTrigger>
                <TabsTrigger value="versions">Versions</TabsTrigger>
              </TabsList>

              <TabsContent value="details" className="min-h-0 flex-1 overflow-y-auto p-3">
                <dl className="divide-border divide-y">
                  <Row label="Type" value={current.mime ?? "File"} />
                  <Row label="Size" value={current.size != null ? fmtSize(current.size) : "—"} />
                  <Row label="Owner" value={current.owner ?? "—"} />
                  <Row label="Modified" value={fmtDate(current.modified_at)} />
                  <Row label="Version" value={current.version ?? "—"} />
                </dl>
              </TabsContent>

              <TabsContent value="versions" className="min-h-0 flex-1 overflow-y-auto p-3">
                <VersionList node={current} />
              </TabsContent>
            </Tabs>
          </aside>
        </div>
      </DialogContent>
    </Dialog>
  );
});

function Row({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="flex items-baseline justify-between gap-3 py-1.5">
      <dt className="text-muted-foreground shrink-0 text-[11px]">{label}</dt>
      <dd className="min-w-0 truncate text-right text-xs">{value}</dd>
    </div>
  );
}
