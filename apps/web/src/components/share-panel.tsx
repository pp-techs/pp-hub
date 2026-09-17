import { useState } from "react";
import type { FormEvent } from "react";
import { toast } from "sonner";
import { Copy, Link2, X } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { Input } from "@workspace/ui/components/input";
import { Label } from "@workspace/ui/components/label";
import { Badge } from "@workspace/ui/components/badge";
import { Checkbox } from "@workspace/ui/components/checkbox";
import { Avatar, AvatarFallback } from "@workspace/ui/components/avatar";
import { ToggleGroup, ToggleGroupItem } from "@workspace/ui/components/toggle-group";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@workspace/ui/components/dialog";
import { Separator } from "@workspace/ui/components/separator";
import type { Node, Share } from "@/lib/api";
import { sharesOptions, useCreateShare, useRevokeShare } from "@/lib/queries";
import { useUiStore } from "@/lib/ui-store";

export function SharePanel({ node }: { node: Node }) {
  const close = useUiStore((s) => s.closePanel);
  const shares = sharesOptions(node.id);
  const create = useCreateShare();
  const revoke = useRevokeShare();

  const [email, setEmail] = useState("");
  const [directLevel, setDirectLevel] = useState<"view" | "edit">("edit");
  const [directCanDownload, setDirectCanDownload] = useState(true);
  const [directCanShare, setDirectCanShare] = useState(true);
  const [linkCanDownload, setLinkCanDownload] = useState(false);
  const [expires, setExpires] = useState("0");

  const fail = (e: unknown) => toast.error(String((e as Error).message ?? e));

  // For direct shares: edit implies download and share
  const effectiveCanDownload = directLevel === "edit" ? true : directCanDownload;
  const effectiveCanShare = directLevel === "edit" ? true : directCanShare;

  return (
    <Dialog open onOpenChange={(v) => !v && close()}>
      <DialogContent className="max-h-[85svh] overflow-y-auto sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>Share "{node.name}"</DialogTitle>
          <DialogDescription>
            Choose who can access this item and what they can do with it.
          </DialogDescription>
        </DialogHeader>

        <section className="rounded-lg border bg-muted/20 p-4">
          <div className="mb-4">
            <h3 className="text-sm font-semibold">Invite people</h3>
            <p className="text-muted-foreground mt-1 text-xs">
              Add a collaborator and choose the access they should have.
            </p>
          </div>
          <form
            className="flex flex-col gap-3"
            onSubmit={(e: FormEvent) => {
              e.preventDefault();
              if (!email.trim()) {
                toast.error("Email required");
                return;
              }
              create.mutate(
                {
                  nodeId: node.id,
                  body: {
                    kind: "direct",
                    level: directLevel,
                    can_download: effectiveCanDownload,
                    can_share: effectiveCanShare,
                    email: email.trim(),
                  },
                },
                {
                  onSuccess: () => {
                    setEmail("");
                    toast.success("Shared");
                  },
                  onError: fail,
                },
              );
            }}
          >
            <div className="grid gap-3 sm:grid-cols-[1fr_auto] sm:items-end">
              <div className="grid gap-1.5">
                <Label htmlFor="share-email">Person or group email</Label>
                <Input
                  id="share-email"
                  type="email"
                  placeholder="name@example.com"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                />
              </div>
              <div className="grid gap-2">
                <Label id="direct-level-label">Access level</Label>
                <ToggleGroup
                  aria-labelledby="direct-level-label"
                  size="sm"
                  variant="outline"
                  value={[directLevel]}
                  onValueChange={(val) => {
                    if (val.length > 0) {
                      setDirectLevel(val[0] as "view" | "edit");
                      // When switching to edit, enable sharing
                      if (val[0] === "edit") {
                        setDirectCanShare(true);
                      }
                    }
                  }}
                >
                  <ToggleGroupItem value="view">Can view</ToggleGroupItem>
                  <ToggleGroupItem value="edit">Can edit</ToggleGroupItem>
                </ToggleGroup>
              </div>
            </div>
            <div className="flex flex-col gap-2 pl-4">
              <div className="flex items-center gap-2">
                <Checkbox
                  id="direct-download"
                  checked={directLevel === "edit" ? true : directCanDownload}
                  disabled={directLevel === "edit"}
                  onCheckedChange={(v) => setDirectCanDownload(Boolean(v))}
                />
                <Label htmlFor="direct-download" className="text-sm font-normal">
                  Allow download
                  {directLevel === "edit" && (
                    <span className="text-muted-foreground ml-1 text-xs">(included with edit)</span>
                  )}
                </Label>
              </div>
              <div className="flex items-center gap-2">
                <Checkbox
                  id="direct-share"
                  checked={directLevel === "edit" ? true : directCanShare}
                  disabled={directLevel === "edit"}
                  onCheckedChange={(v) => setDirectCanShare(Boolean(v))}
                />
                <Label htmlFor="direct-share" className="text-sm font-normal">
                  Allow sharing
                  {directLevel === "edit" && (
                    <span className="text-muted-foreground ml-1 text-xs">(included with edit)</span>
                  )}
                </Label>
              </div>
            </div>
            <Button type="submit" className="self-start" disabled={create.isPending}>
              Share
            </Button>
          </form>
        </section>

        <Separator />

        <section className="rounded-lg border bg-muted/20 p-4">
          <div className="mb-4">
            <h3 className="text-sm font-semibold">Public link</h3>
            <p className="text-muted-foreground mt-1 text-xs">
              Anyone holding the link can view this item without signing in. A link never carries
              editing, and nobody can re-share it onward — download is the one choice it offers.
            </p>
          </div>
          <form
            className="flex flex-col gap-3"
            onSubmit={(e: FormEvent) => {
              e.preventDefault();
              create.mutate(
                {
                  nodeId: node.id,
                  body: {
                    kind: "link",
                    level: "view",
                    can_download: linkCanDownload,
                    can_share: false,
                    expires_in: Number(expires) || 0,
                  },
                },
                {
                  onSuccess: (sh: Share) => {
                    if (sh.url) {
                      toast.success("Link created (shown once)", {
                        action: {
                          label: "Copy",
                          onClick: () => void navigator.clipboard.writeText(sh.url!),
                        },
                      });
                    }
                  },
                  onError: fail,
                },
              );
            }}
          >
            <div className="grid gap-3 sm:grid-cols-2">
              <div className="grid gap-1.5">
                <Label htmlFor="link-download-toggle">Access</Label>
                <div className="flex items-center gap-2 pt-1.5">
                  <Checkbox
                    id="link-download-toggle"
                    checked={linkCanDownload}
                    onCheckedChange={(v) => setLinkCanDownload(Boolean(v))}
                  />
                  <Label htmlFor="link-download-toggle" className="text-sm font-normal">
                    Allow download
                  </Label>
                </div>
              </div>
              <div className="grid gap-1.5">
                <Label htmlFor="link-expiry">Expiration</Label>
                <Input
                  id="link-expiry"
                  type="number"
                  min={0}
                  value={expires}
                  onChange={(e) => setExpires(e.target.value)}
                  aria-describedby="link-expiry-help"
                />
                <span id="link-expiry-help" className="text-muted-foreground text-xs">
                  Seconds; use 0 for no expiration.
                </span>
              </div>
            </div>
            <Button
              type="submit"
              variant="outline"
              className="self-start"
              disabled={create.isPending}
            >
              Create public link
            </Button>
          </form>
        </section>

        <Separator />

        <section className="grid gap-2">
          <div>
            <h3 className="text-sm font-semibold">Current access</h3>
            <p className="text-muted-foreground mt-1 text-xs">
              Review who can access this item and revoke access when needed.
            </p>
          </div>

          <div className="rounded-md border bg-background">
            {shares.isPending ? (
              <p className="text-muted-foreground p-3 text-sm">Loading…</p>
            ) : shares.data?.length === 0 ? (
              <p className="text-muted-foreground p-3 text-sm">No one else has access yet.</p>
            ) : (
              <ul className="divide-y">
                {shares.data?.map((s) => (
                  <li key={s.id} className="flex items-center gap-3 p-3 text-sm">
                    <Avatar size="sm">
                      <AvatarFallback>
                        {s.grantee_email ? (
                          s.grantee_email.charAt(0).toUpperCase()
                        ) : (
                          <Link2 className="size-3.5" />
                        )}
                      </AvatarFallback>
                    </Avatar>
                    <div className="min-w-0 flex-1">
                      <p className="truncate font-medium">
                        {s.grantee_email || (s.kind === "link" ? "Anyone with the link" : s.kind)}
                      </p>
                      <p className="text-muted-foreground flex items-center gap-2 text-xs">
                        <span className="text-muted-foreground text-[10px] uppercase">
                          {s.kind === "link" ? "Public link" : "Direct invitation"}
                        </span>
                      </p>
                    </div>
                    <Badge variant="outline" className="shrink-0 capitalize">
                      {s.level}
                    </Badge>
                    <div className="flex items-center gap-1">
                      {s.can_download && (
                        <span className="text-muted-foreground text-xs" title="Can download">
                          ↓
                        </span>
                      )}
                      {s.can_share && (
                        <span className="text-muted-foreground text-xs" title="Can share">
                          ↗
                        </span>
                      )}
                    </div>
                    {s.url ? (
                      <a
                        href={s.url}
                        target="_blank"
                        rel="noreferrer"
                        className="text-primary inline-flex items-center gap-1 text-xs underline"
                      >
                        <Copy className="size-3.5" /> Open
                      </a>
                    ) : null}
                    <Button
                      variant="ghost"
                      size="icon-xs"
                      className="shrink-0"
                      aria-label={`Revoke access for ${s.grantee_email || "public link"}`}
                      onClick={() =>
                        revoke.mutate(
                          { nodeId: node.id, shareId: s.id },
                          { onSuccess: () => toast.success("Access revoked"), onError: fail },
                        )
                      }
                    >
                      <X />
                    </Button>
                  </li>
                ))}
              </ul>
            )}
          </div>
        </section>
      </DialogContent>
    </Dialog>
  );
}
