import { Trans, useLingui } from "@lingui/react/macro";
import { createModal } from "@buiducnhat/better-modal";
import { toast } from "sonner";
import { useQueryClient } from "@tanstack/react-query";
import { Copy } from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import {
  Dialog,
  DialogContent,
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
import { Switch } from "@workspace/ui/components/switch";
import { Textarea } from "@workspace/ui/components/textarea";
import type { AgentToken, UserPreferences, UserProfile } from "@/lib/api";
import { updatePreferences } from "@/lib/api/users";
import {
  keys,
  useAgentTokens,
  useCreateAgentToken,
  useMe,
  useRevokeAgentToken,
} from "@/lib/queries";
import { useFormatters } from "@/lib/format";
import { useKeyedState } from "@/lib/use-keyed-state";
import { SectionLabel } from "@/components/section-label";
import { LoadingStatus } from "@/components/loading";

/** Lifetimes offered for a new token; the server accepts 1–90 days. */
const EXPIRY_DAYS = ["7", "30", "90"] as const;

/** Writes `text` to the clipboard and says so either way. */
function copyWithToast(text: string, done: string, failed: string) {
  void navigator.clipboard.writeText(text).then(
    () => toast.success(done),
    () => toast.error(failed),
  );
}

interface CreateTokenProps extends Record<string, unknown> {
  mcpUrl: string;
  /** Changes on every open, so a reopened dialog starts from an empty form. */
  openedAt: number;
}

/**
 * Names and mints a token, then — in the same dialog — shows the secret once
 * with a `.mcp.json` snippet around it. The server never returns the secret
 * again, so this step is the only chance to copy it.
 */
const CreateTokenDialog = createModal<CreateTokenProps>(
  "create-agent-token",
  ({ mcpUrl, openedAt, modal }) => {
    const { t } = useLingui();
    const create = useCreateAgentToken();
    const [name, setName] = useKeyedState(openedAt, "");
    const [days, setDays] = useKeyedState<number, string>(openedAt, "30");
    const [secret, setSecret] = useKeyedState<number, string | null>(openedAt, null);

    const trimmed = name.trim();
    const close = () => modal.resolve(undefined);

    const submit = () =>
      create.mutate(
        { name: trimmed, expires_in_days: Number(days) },
        {
          onSuccess: (created) => setSecret(created.token),
          onError: (e) => toast.error(t`Could not create the token`, { description: e.message }),
        },
      );

    const snippet = secret
      ? JSON.stringify(
          {
            mcpServers: {
              "pp-hub": {
                type: "http",
                url: mcpUrl,
                // eslint-disable-next-line lingui/no-unlocalized-strings -- HTTP header value
                headers: { Authorization: `Bearer ${secret}` },
              },
            },
          },
          null,
          2,
        )
      : "";

    return (
      <Dialog open={modal.visible} onOpenChange={(open) => !open && close()}>
        <DialogContent className="sm:max-w-lg">
          {secret ? (
            <>
              <DialogHeader>
                <DialogTitle>
                  <Trans>Copy your token now</Trans>
                </DialogTitle>
                <DialogDescription>
                  <Trans>It will not be shown again. Anyone holding it can act as you.</Trans>
                </DialogDescription>
              </DialogHeader>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="agent-token-secret">
                  <Trans>Token</Trans>
                </Label>
                <div className="flex items-center gap-2">
                  <Input
                    id="agent-token-secret"
                    readOnly
                    value={secret}
                    className="font-mono"
                    onFocus={(e) => e.currentTarget.select()}
                  />
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() =>
                      copyWithToast(secret, t`Token copied`, t`Could not copy the token`)
                    }
                  >
                    <Copy />
                    <Trans>Copy</Trans>
                  </Button>
                </div>
              </div>
              <div className="flex flex-col gap-1.5">
                <div className="flex items-center justify-between gap-2">
                  <Label htmlFor="agent-token-snippet">
                    <Trans>.mcp.json</Trans>
                  </Label>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() =>
                      copyWithToast(snippet, t`Snippet copied`, t`Could not copy the snippet`)
                    }
                  >
                    <Copy />
                    <Trans>Copy</Trans>
                  </Button>
                </div>
                <Textarea
                  id="agent-token-snippet"
                  readOnly
                  value={snippet}
                  rows={10}
                  className="font-mono text-[11px]"
                  onFocus={(e) => e.currentTarget.select()}
                />
              </div>
              <DialogFooter>
                <Button onClick={close}>
                  <Trans>Done</Trans>
                </Button>
              </DialogFooter>
            </>
          ) : (
            <form
              className="flex flex-col gap-4"
              onSubmit={(e) => {
                e.preventDefault();
                if (trimmed) submit();
              }}
            >
              <DialogHeader>
                <DialogTitle>
                  <Trans>Create a personal token</Trans>
                </DialogTitle>
                <DialogDescription>
                  <Trans>Name it after the agent or machine that will use it.</Trans>
                </DialogDescription>
              </DialogHeader>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="agent-token-name">
                  <Trans>Name</Trans>
                </Label>
                <Input
                  id="agent-token-name"
                  value={name}
                  maxLength={100}
                  placeholder={t`Claude Code on my laptop`}
                  autoFocus
                  onChange={(e) => setName(e.target.value)}
                />
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="agent-token-expiry">
                  <Trans>Expires after</Trans>
                </Label>
                <Select
                  value={days}
                  items={EXPIRY_DAYS.map((d) => ({ value: d, label: t`${d} days` }))}
                  onValueChange={(v) => setDays((v as string | null) ?? "30")}
                >
                  <SelectTrigger id="agent-token-expiry" className="w-full">
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
                <Button type="button" variant="outline" onClick={close}>
                  <Trans>Cancel</Trans>
                </Button>
                <Button type="submit" disabled={!trimmed || create.isPending}>
                  <Trans>Create token</Trans>
                </Button>
              </DialogFooter>
            </form>
          )}
        </DialogContent>
      </Dialog>
    );
  },
);

/**
 * Whether AI agents (Claude, ChatGPT) may act as this account over MCP, the
 * connector URL to give them, and personal tokens for agents that cannot sign
 * in through a browser. Without the URL the administrator has not turned
 * agent access on, so there is nothing to switch.
 */
export function AgentAccessSection() {
  const { t } = useLingui();
  const qc = useQueryClient();
  const { data: me } = useMe();
  const mcpUrl = me?.mcp_url;

  return (
    <section className="flex flex-col gap-3">
      <SectionLabel>
        <Trans>AI agents</Trans>
      </SectionLabel>
      {mcpUrl ? (
        <>
          <div className="flex items-center justify-between gap-4">
            <Label
              htmlFor="pref-agents-enabled"
              className="flex-col items-start gap-0.5 font-normal"
            >
              <Trans>Let AI agents act as me</Trans>
              <span className="text-muted-foreground block text-[10px]">
                <Trans>
                  Add this URL as a custom connector in Claude or ChatGPT and sign in with your PP
                  Hub account. The agent can do only what you can.
                </Trans>
              </span>
            </Label>
            <Switch
              id="pref-agents-enabled"
              checked={me?.preferences.agents_enabled !== false}
              onCheckedChange={(v) => {
                updatePreferences({ agents_enabled: v })
                  .then((updated: UserPreferences) => {
                    qc.setQueryData<UserProfile>(keys.me, (prev) =>
                      prev ? { ...prev, preferences: updated } : prev,
                    );
                  })
                  .catch(() => {
                    toast.error(t`Could not save your agent setting.`);
                  });
              }}
            />
          </div>
          <div className="flex items-center gap-2">
            <Input
              readOnly
              value={mcpUrl}
              aria-label={t`Connector URL`}
              onFocus={(e) => e.currentTarget.select()}
            />
            <Button
              variant="outline"
              size="sm"
              onClick={() =>
                copyWithToast(mcpUrl, t`Connector URL copied`, t`Could not copy the connector URL`)
              }
            >
              <Copy />
              <Trans>Copy</Trans>
            </Button>
          </div>
          <PersonalTokens mcpUrl={mcpUrl} canCreate={me?.mcp_tokens === true} />
        </>
      ) : (
        <p className="text-muted-foreground text-[10px]">
          <Trans>An administrator has not turned on agent access.</Trans>
        </p>
      )}
    </section>
  );
}

function PersonalTokens({ mcpUrl, canCreate }: { mcpUrl: string; canCreate: boolean }) {
  const { t } = useLingui();
  const qc = useQueryClient();
  const { data: tokens, isLoading, isError } = useAgentTokens();
  const revoke = useRevokeAgentToken();

  async function createToken() {
    await CreateTokenDialog.show({ mcpUrl, openedAt: Date.now() });
    void qc.invalidateQueries({ queryKey: keys.agentTokens });
  }

  return (
    <div className="flex flex-col gap-3 pt-2">
      <div className="flex items-start justify-between gap-4">
        <div className="flex flex-col gap-0.5">
          <span className="text-xs font-medium">
            <Trans>Personal tokens</Trans>
          </span>
          <p className="text-muted-foreground text-[10px]">
            <Trans>
              For agents that cannot sign in through a browser, such as Claude Code or Cursor via
              .mcp.json, or CI. A token acts as you and expires.
            </Trans>
          </p>
        </div>
        {canCreate && (
          <Button
            variant="outline"
            size="sm"
            className="shrink-0"
            onClick={() => void createToken()}
          >
            <Trans>Create token</Trans>
          </Button>
        )}
      </div>
      {!canCreate && (
        <p className="text-muted-foreground text-[10px]">
          <Trans>An administrator has turned personal tokens off.</Trans>
        </p>
      )}

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
    </div>
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
