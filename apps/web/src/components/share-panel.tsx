import { useState } from "react";
import type { FormEvent } from "react";
import { toast } from "sonner";
import { Copy, Link2, X } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { Input } from "@workspace/ui/components/input";
import { Label } from "@workspace/ui/components/label";
import { Badge } from "@workspace/ui/components/badge";
import { Avatar, AvatarFallback } from "@workspace/ui/components/avatar";
import { ToggleGroup, ToggleGroupItem } from "@workspace/ui/components/toggle-group";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@workspace/ui/components/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@workspace/ui/components/select";
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
  const [directPerm, setDirectPerm] = useState("download");
  const [linkPerm, setLinkPerm] = useState("download");
  const [expires, setExpires] = useState("0");

  const fail = (e: unknown) => toast.error(String((e as Error).message ?? e));

  return (
    <Dialog open onOpenChange={(v) => !v && close()}>
      <DialogContent className="max-h-[85svh] overflow-y-auto sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>Share “{node.name}”</DialogTitle>
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
                  body: { kind: "direct", perm: directPerm, email: email.trim() },
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
              <div className="grid gap-1.5">
                <Label htmlFor="direct-permission">Permission</Label>
                <ToggleGroup
                  id="direct-permission"
                  size="sm"
                  variant="outline"
                  value={[directPerm]}
                  onValueChange={(val) => {
                    if (val.length > 0) setDirectPerm(val[0]);
                  }}
                >
                  <ToggleGroupItem value="view">Viewer</ToggleGroupItem>
                  <ToggleGroupItem value="download">Download</ToggleGroupItem>
                  <ToggleGroupItem value="edit">Editor</ToggleGroupItem>
                </ToggleGroup>
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
              Anyone with this link can access the item using the selected permission.
            </p>
          </div>
          <form
            className="flex flex-col gap-3"
            onSubmit={(e: FormEvent) => {
              e.preventDefault();
              create.mutate(
                {
                  nodeId: node.id,
                  body: { kind: "link", perm: linkPerm, expires_in: Number(expires) || 0 },
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
                <Label htmlFor="link-permission">Permission</Label>
                <PermSelect value={linkPerm} onChange={setLinkPerm} />
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
                      <p className="text-muted-foreground text-xs">
                        {s.kind === "link" ? "Public link" : "Direct invitation"}
                      </p>
                    </div>
                    <Badge variant="outline" className="shrink-0 capitalize">
                      {s.perm}
                    </Badge>
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

function PermSelect({
  value,
  onChange,
  allowEdit = false,
}: {
  value: string;
  onChange: (v: string) => void;
  allowEdit?: boolean;
}) {
  const permItems = [
    { label: "view", value: "view" },
    { label: "download", value: "download" },
    ...(allowEdit ? [{ label: "edit", value: "edit" }] : []),
  ];
  return (
    <Select value={value} items={permItems} onValueChange={(v) => onChange(v ?? value)}>
      <SelectTrigger size="sm" className="w-28">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {permItems.map((item) => (
          <SelectItem key={item.value} value={item.value}>
            {item.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
