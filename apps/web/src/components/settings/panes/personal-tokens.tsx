import type * as React from "react";
import { Trans, useLingui } from "@lingui/react/macro";
import { toast } from "sonner";
import { Copy } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import {
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@workspace/ui/components/dialog";
import { Input } from "@workspace/ui/components/input";
import { Label } from "@workspace/ui/components/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@workspace/ui/components/select";
import { Skeleton } from "@workspace/ui/components/skeleton";
import type { AgentToken, AgentTokenPurpose } from "@/lib/api";
import { useAgentTokens, useRevokeAgentToken } from "@/lib/queries";
import { useFormatters } from "@/lib/format";
import { useKeyedState } from "@/lib/use-keyed-state";
import { LoadingStatus } from "@/components/loading";

/*
 * The pieces both kinds of personal token share — MCP tokens for agents and
 * WebDAV tokens for mounting the drive. They are the same server resource
 * told apart by `purpose`, so they are named, listed and revoked the same way.
 */

/** Lifetimes offered for a new token; the server accepts 1–90 days. */
const EXPIRY_DAYS = ["7", "30", "90"] as const;

/** Writes `text` to the clipboard and says so either way. */
export function copyWithToast(text: string, done: string, failed: string) {
  void navigator.clipboard.writeText(text).then(
    () => toast.success(done),
    () => toast.error(failed),
  );
}

/** A read-only value with a label and a Copy button beside it. */
export function CopyField({
  id,
  label,
  value,
  copied,
  copyFailed,
}: {
  id: string;
  label: React.ReactNode;
  value: string;
  copied: string;
  copyFailed: string;
}) {
  return (
    <div className="flex flex-col gap-1.5">
      <Label htmlFor={id}>{label}</Label>
      <div className="flex items-center gap-2">
        <Input
          id={id}
          readOnly
          value={value}
          className="font-mono"
          onFocus={(e) => e.currentTarget.select()}
        />
        <Button
          variant="outline"
          size="sm"
          onClick={() => copyWithToast(value, copied, copyFailed)}
        >
          <Copy />
          <Trans>Copy</Trans>
        </Button>
      </div>
    </div>
  );
}

/**
 * The first step of a create-token dialog: a name and a lifetime. It lives
 * inside the caller's `DialogContent`; `openedAt` changes on every open, so a
 * reopened dialog starts from an empty form.
 */
export function CreateTokenForm({
  idPrefix,
  openedAt,
  title,
  description,
  placeholder,
  pending,
  onSubmit,
  onCancel,
}: {
  idPrefix: string;
  openedAt: number;
  title: React.ReactNode;
  description: React.ReactNode;
  placeholder: string;
  pending: boolean;
  onSubmit: (name: string, expiresInDays: number) => void;
  onCancel: () => void;
}) {
  const { t } = useLingui();
  const [name, setName] = useKeyedState(openedAt, "");
  const [days, setDays] = useKeyedState<number, string>(openedAt, "30");
  const trimmed = name.trim();

  return (
    <form
      className="flex flex-col gap-4"
      onSubmit={(e) => {
        e.preventDefault();
        if (trimmed) onSubmit(trimmed, Number(days));
      }}
    >
      <DialogHeader>
        <DialogTitle>{title}</DialogTitle>
        <DialogDescription>{description}</DialogDescription>
      </DialogHeader>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor={`${idPrefix}-name`}>
          <Trans>Name</Trans>
        </Label>
        <Input
          id={`${idPrefix}-name`}
          value={name}
          maxLength={100}
          placeholder={placeholder}
          autoFocus
          onChange={(e) => setName(e.target.value)}
        />
      </div>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor={`${idPrefix}-expiry`}>
          <Trans>Expires after</Trans>
        </Label>
        <Select
          value={days}
          items={EXPIRY_DAYS.map((d) => ({ value: d, label: t`${d} days` }))}
          onValueChange={(v) => setDays((v as string | null) ?? "30")}
        >
          <SelectTrigger id={`${idPrefix}-expiry`} className="w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {EXPIRY_DAYS.map((d) => (
              <SelectItem key={d} value={d}>
                <Trans>{d} days</Trans>
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onCancel}>
          <Trans>Cancel</Trans>
        </Button>
        <Button type="submit" disabled={!trimmed || pending}>
          <Trans>Create token</Trans>
        </Button>
      </DialogFooter>
    </form>
  );
}

/** The caller's live tokens of one purpose, each with a Revoke button. */
export function TokenList({ purpose }: { purpose: AgentTokenPurpose }) {
  const { t } = useLingui();
  const { data: tokens, isLoading, isError } = useAgentTokens(purpose);
  const revoke = useRevokeAgentToken();

  return (
    <>
      {isLoading && (
        <LoadingStatus>
          <ul className="border-border divide-border divide-y rounded-md border">
            {[40, 55].map((width) => (
              <li key={width} className="flex flex-col gap-1.5 px-3 py-2.5">
                <Skeleton className="h-3.5" style={{ width: `${width}%` }} />
                <Skeleton className="h-2.5" style={{ width: `${width + 20}%` }} />
              </li>
            ))}
          </ul>
        </LoadingStatus>
      )}
      {isError && (
        <p className="text-destructive text-xs">
          <Trans>Could not load your tokens.</Trans>
        </p>
      )}

      {tokens && tokens.length > 0 && (
        <ul className="border-border divide-border divide-y rounded-md border">
          {tokens.map((token) => (
            <TokenRow
              key={token.id}
              token={token}
              busy={revoke.isPending && revoke.variables === token.id}
              onRevoke={() =>
                revoke.mutate(token.id, {
                  onSuccess: () => toast.success(t`Token revoked`),
                  onError: (e) =>
                    toast.error(t`Could not revoke that token`, { description: e.message }),
                })
              }
            />
          ))}
        </ul>
      )}
    </>
  );
}

function TokenRow({
  token,
  busy,
  onRevoke,
}: {
  token: AgentToken;
  busy: boolean;
  onRevoke: () => void;
}) {
  const f = useFormatters();
  const created = f.date(token.created_at);
  const expires = f.date(token.expires_at);
  const lastUsed = token.last_used_at ? f.relative(token.last_used_at) : null;

  return (
    <li className="flex items-center gap-3 px-3 py-2.5">
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="truncate text-xs font-medium">{token.name}</span>
        <span className="text-muted-foreground truncate text-[10px]">
          {lastUsed ? (
            <Trans>
              created {created} · expires {expires} · last used {lastUsed}
            </Trans>
          ) : (
            <Trans>
              created {created} · expires {expires} · never used
            </Trans>
          )}
        </span>
      </div>
      <Button variant="ghost" size="sm" className="shrink-0" disabled={busy} onClick={onRevoke}>
        <Trans>Revoke</Trans>
      </Button>
    </li>
  );
}
