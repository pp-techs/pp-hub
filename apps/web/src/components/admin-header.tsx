import { Link } from "@tanstack/react-router";
import { ArrowLeft } from "lucide-react";
import { Trans, useLingui } from "@lingui/react/macro";
import type * as React from "react";
import { Button } from "@workspace/ui/components/button";
import { cn } from "@workspace/ui/lib/utils";

interface AdminHeaderProps {
  title: React.ReactNode;
  subtitle?: React.ReactNode;
  activeTab: "users" | "security" | "settings";
}

export function AdminHeader({ title, subtitle, activeTab }: AdminHeaderProps) {
  const { t } = useLingui();

  return (
    <header className="border-border flex h-12 shrink-0 items-center gap-2 border-b px-4">
      <Button
        variant="ghost"
        size="icon-sm"
        aria-label={t`Back to Drive`}
        render={<Link to="/drive" />}
      >
        <ArrowLeft />
      </Button>
      <div className="flex flex-col">
        <span className="text-sm font-medium">{title}</span>
        {subtitle && <span className="text-muted-foreground text-[10px]">{subtitle}</span>}
      </div>

      <nav className="ml-auto flex items-center gap-1" aria-label={t`Admin sections`}>
        <Button
          variant="ghost"
          size="sm"
          className={cn(activeTab === "users" && "bg-muted font-medium")}
          render={<Link to="/admin/users" />}
        >
          <Trans>Users</Trans>
        </Button>
        <Button
          variant="ghost"
          size="sm"
          className={cn(activeTab === "security" && "bg-muted font-medium")}
          render={<Link to="/admin/security" />}
        >
          <Trans>Security</Trans>
        </Button>
        <Button
          variant="ghost"
          size="sm"
          className={cn(activeTab === "settings" && "bg-muted font-medium")}
          render={<Link to="/admin/settings" />}
        >
          <Trans>Settings</Trans>
        </Button>
      </nav>
    </header>
  );
}
