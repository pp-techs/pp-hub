import { useMutation, useQuery } from "@tanstack/react-query";
import { Trans, useLingui } from "@lingui/react/macro";
import { Mail } from "lucide-react";
import { useState } from "react";
import type { FormEvent } from "react";
import { Alert, AlertDescription } from "@workspace/ui/components/alert";
import { Button } from "@workspace/ui/components/button";
import { Input } from "@workspace/ui/components/input";
import { Label } from "@workspace/ui/components/label";
import { Separator } from "@workspace/ui/components/separator";
import { ApiError, getGuestConfig, requestGuestCode, verifyGuestCode } from "@/lib/api";

// eslint-disable-next-line lingui/no-unlocalized-strings -- a route, not text
const DRIVE_PATH = "/drive";

/**
 * Opens what was shared with an email address by proving the address with a
 * one-time code: no account, no password. Offered under SSO on the sign-in page
 * only while the instance admits guests (`GET /auth/guest`).
 *
 * The server answers "code requested" the same way whether or not the address
 * has anything shared with it, so the second step is shown either way — the
 * form says "if there is something for this address", never "sent".
 */
export function GuestSignIn({ redirectTo }: { redirectTo: string | undefined }) {
  const { t } = useLingui();
  const config = useQuery({ queryKey: ["guest-config"], queryFn: getGuestConfig, retry: false });
  const [email, setEmail] = useState("");
  const [code, setCode] = useState("");
  const [asked, setAsked] = useState(false);

  const ask = useMutation({
    mutationFn: () => requestGuestCode(email.trim()),
    onSuccess: () => setAsked(true),
  });
  const verify = useMutation({
    mutationFn: () => verifyGuestCode(email.trim(), code.trim()),
    // A full navigation, not a router one: every cached query belongs to
    // nobody, and the shell should start from the new session.
    onSuccess: () => window.location.assign(redirectTo ?? DRIVE_PATH),
  });

  if (!config.data?.enabled) return null;

  const failure = ask.error ?? verify.error;
  const message =
    failure instanceof ApiError && failure.status === 429
      ? t`Too many attempts. Try again in a little while.`
      : verify.error
        ? t`That code is not valid or has expired. Ask for a new one.`
        : failure
          ? t`Could not ask for a code. Try again in a moment.`
          : null;

  return (
    <>
      <div className="flex items-center gap-3">
        <Separator className="flex-1" />
        <span className="text-muted-foreground text-[10px] uppercase">
          <Trans>or</Trans>
        </span>
        <Separator className="flex-1" />
      </div>
      <form
        className="flex flex-col gap-3"
        onSubmit={(e: FormEvent) => {
          e.preventDefault();
          if (asked) verify.mutate();
          else ask.mutate();
        }}
      >
        <div className="grid gap-0.5">
          <span className="text-xs font-medium">
            <Trans>Open something shared with your email</Trans>
          </span>
          <span className="text-muted-foreground text-xs/relaxed">
            <Trans>No account needed. We send a code to the address it was shared with.</Trans>
          </span>
        </div>
        <div className="flex flex-col gap-1">
          <Label htmlFor="guest-email">
            <Trans>Email address</Trans>
          </Label>
          <Input
            id="guest-email"
            type="email"
            autoComplete="email"
            required
            disabled={asked}
            value={email}
            onChange={(e) => setEmail(e.target.value)}
          />
        </div>
        {asked && (
          <div className="flex flex-col gap-1">
            <Label htmlFor="guest-code">
              <Trans>Code</Trans>
            </Label>
            <Input
              id="guest-code"
              inputMode="numeric"
              autoComplete="one-time-code"
              autoFocus
              required
              maxLength={6}
              value={code}
              onChange={(e) => setCode(e.target.value.replace(/\D/g, ""))}
            />
            <p className="text-muted-foreground text-[10px]">
              <Trans>
                If something was shared with this address, a code is on its way. It works for ten
                minutes.
              </Trans>
            </p>
          </div>
        )}
        {message && (
          <Alert variant="destructive">
            <AlertDescription>{message}</AlertDescription>
          </Alert>
        )}
        <div className="flex gap-2">
          <Button
            type="submit"
            variant="outline"
            className="flex-1"
            disabled={ask.isPending || verify.isPending || (asked && code.length !== 6)}
          >
            <Mail data-icon="inline-start" />
            {asked ? <Trans>Open</Trans> : <Trans>Email me a code</Trans>}
          </Button>
          {asked && (
            <Button
              type="button"
              variant="ghost"
              onClick={() => {
                setAsked(false);
                setCode("");
                ask.reset();
                verify.reset();
              }}
            >
              <Trans>Start over</Trans>
            </Button>
          )}
        </div>
      </form>
    </>
  );
}
