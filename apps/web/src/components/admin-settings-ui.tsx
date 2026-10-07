import type * as React from "react";
import { RotateCcw } from "lucide-react";
import { Trans } from "@lingui/react/macro";
import { Badge } from "@workspace/ui/components/badge";
import { Button } from "@workspace/ui/components/button";
import { Card } from "@workspace/ui/components/card";
import { Input } from "@workspace/ui/components/input";
import { cn } from "@workspace/ui/lib/utils";
import type { AdminSetting } from "@/lib/api";

/**
 * Where a setting's current value comes from. An override in the database is
 * the one an admin set here, so it is the only filled badge: it is also the
 * only source that can be reset.
 */
export function SourceBadge({ source }: { source: AdminSetting["source"] }) {
  if (source === "database") {
    return (
      <Badge variant="default">
        <Trans>Database override</Trans>
      </Badge>
    );
  }
  if (source === "env") {
    return (
      <Badge variant="secondary">
        <Trans>Environment variable</Trans>
      </Badge>
    );
  }
  return (
    <Badge variant="outline">
      <Trans>Default</Trans>
    </Badge>
  );
}

/**
 * One group of related settings: a heading and what the group is for, over a
 * single card whose rows are the settings. Rows divide the card rather than
 * each being a card, so a long page reads as a handful of groups instead of a
 * wall of boxes.
 */
export function SettingsSection({
  title,
  description,
  children,
  footer,
}: {
  title: React.ReactNode;
  description?: React.ReactNode;
  children: React.ReactNode;
  footer?: React.ReactNode;
}) {
  return (
    <section className="flex flex-col gap-3">
      <div className="flex flex-col gap-0.5 px-0.5">
        <h2 className="text-sm font-semibold">{title}</h2>
        {description && <p className="text-muted-foreground text-xs">{description}</p>}
      </div>
      <Card className="gap-0 divide-y py-0">
        {children}
        {footer}
      </Card>
    </section>
  );
}

/**
 * One setting: what it is on the left, the control that changes it on the
 * right, stacking on a narrow screen. The Reset sits with the control because
 * it acts on the control's value.
 */
export function SettingRow({
  title,
  description,
  source,
  onReset,
  resetDisabled,
  children,
  htmlFor,
  className,
}: {
  title: React.ReactNode;
  description?: React.ReactNode;
  source?: AdminSetting["source"];
  /** Present only for a database override; the caller decides when. */
  onReset?: () => void;
  resetDisabled?: boolean;
  children?: React.ReactNode;
  /** The control's id, so clicking the title focuses it. */
  htmlFor?: string;
  className?: string;
}) {
  return (
    <div
      className={cn(
        "flex flex-col gap-3 p-4 sm:flex-row sm:items-center sm:justify-between sm:gap-8",
        className,
      )}
    >
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <div className="flex flex-wrap items-center gap-2">
          <label htmlFor={htmlFor} className="text-sm font-medium">
            {title}
          </label>
          {source && <SourceBadge source={source} />}
        </div>
        {description && (
          <p className="text-muted-foreground max-w-prose text-xs/relaxed">{description}</p>
        )}
      </div>
      {(onReset || children) && (
        <div className="flex shrink-0 items-center gap-2 sm:justify-end">
          {onReset && (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={onReset}
              disabled={resetDisabled}
            >
              <RotateCcw className="size-3.5" />
              <Trans>Reset</Trans>
            </Button>
          )}
          {children}
        </div>
      )}
    </div>
  );
}

/** A number field with its unit beside it, at a width that fits the value. */
export function UnitInput({
  unit,
  className,
  ...props
}: Omit<React.ComponentProps<typeof Input>, "type"> & { unit?: React.ReactNode }) {
  return (
    <div className="flex items-center gap-2">
      <Input type="number" className={cn("w-28 tabular-nums", className)} {...props} />
      {unit && <span className="text-muted-foreground w-12 text-xs">{unit}</span>}
    </div>
  );
}
