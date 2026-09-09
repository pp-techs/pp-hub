import { useState } from "react";
import type { FormEvent } from "react";
import { toast } from "sonner";
import { Copy, X } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { Input } from "@workspace/ui/components/input";
import { Label } from "@workspace/ui/components/label";
import { Badge } from "@workspace/ui/components/badge";
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
      <DialogContent className="max-h-[85svh] overflow-y-auto sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Share “{node.name}”</DialogTitle>
          <DialogDescription>
            Direct shares grant a user access; link shares are public.
          </DialogDescription>
        </DialogHeader>

        <form
          className="flex flex-col gap-2"
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
          <Label htmlFor="share-email">Share with user</Label>
          <div className="flex gap-2">
            <Input
              id="share-email"
              type="email"
              placeholder="user@example.com"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
            />
            <PermSelect value={directPerm} onChange={setDirectPerm} allowEdit />
            <Button type="submit" disabled={create.isPending}>
              Share
            </Button>
          </div>
        </form>

        <Separator />

        <form
          className="flex flex-col gap-2"
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
          <Label>Public link</Label>
          <div className="flex gap-2">
            <PermSelect value={linkPerm} onChange={setLinkPerm} />
            <Input
              type="number"
              min={0}
              className="w-28"
              value={expires}
              onChange={(e) => setExpires(e.target.value)}
              aria-label="Expires in seconds (0 = never)"
              placeholder="expires (s)"
            />
            <Button type="submit" variant="outline" disabled={create.isPending}>
              Create link
            </Button>
          </div>
        </form>

        <Separator />

        <div className="flex flex-col gap-1">
          <Label>Current shares</Label>
          {shares.isPending ? (
            <p className="text-muted-foreground text-sm">Loading…</p>
          ) : shares.data?.length === 0 ? (
            <p className="text-muted-foreground text-sm">None yet.</p>
          ) : (
            <ul className="flex flex-col gap-1">
              {shares.data?.map((s) => (
                <li key={s.id} className="flex items-center gap-2 text-sm">
                  <Badge variant="outline">{s.kind}</Badge>
                  <span>
                    {s.perm}
                    {s.grantee_email ? ` → ${s.grantee_email}` : ""}
                  </span>
                  {s.url ? (
                    <a
                      href={s.url}
                      target="_blank"
                      rel="noreferrer"
                      className="text-primary inline-flex items-center gap-1 underline"
                    >
                      <Copy className="size-3.5" /> link
                    </a>
                  ) : null}
                  <Button
                    variant="ghost"
                    size="icon-xs"
                    className="ml-auto"
                    aria-label="Revoke share"
                    onClick={() =>
                      revoke.mutate(
                        { nodeId: node.id, shareId: s.id },
                        { onSuccess: () => toast.success("Revoked"), onError: fail },
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
  return (
    <Select
      value={value}
      onValueChange={(v) => onChange(v ?? value)}
      items={{ view: "view", download: "download", edit: "edit" }}
    >
      <SelectTrigger size="sm" className="w-28">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value="view">view</SelectItem>
        <SelectItem value="download">download</SelectItem>
        {allowEdit ? <SelectItem value="edit">edit</SelectItem> : null}
      </SelectContent>
    </Select>
  );
}
