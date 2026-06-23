import Link from "next/link";
import { Activity } from "lucide-react";
import type { Me } from "@/lib/types";
import { OrgSwitcher } from "./org-switcher";
import { UserMenu } from "./user-menu";

// Authenticated top bar: brand, org switcher, user menu. The org-scoped sidebar
// is rendered by the [orgId] layout, so org-agnostic pages (the org picker,
// account) just get the bar + content.
export function AppShell({
  me,
  children,
}: {
  me: Me;
  children: React.ReactNode;
}) {
  return (
    <div className="flex min-h-screen flex-col">
      <header className="sticky top-0 z-30 flex h-14 items-center gap-3 border-b bg-background/95 px-4 backdrop-blur">
        <Link href="/orgs" className="flex items-center gap-2 font-semibold">
          <Activity className="h-5 w-5" />
          <span className="hidden sm:inline">StatusFlow</span>
        </Link>
        <span className="text-muted-foreground">/</span>
        <OrgSwitcher memberships={me.memberships} />
        <div className="ml-auto">
          <UserMenu user={me.user} />
        </div>
      </header>
      <div className="flex flex-1">{children}</div>
    </div>
  );
}
