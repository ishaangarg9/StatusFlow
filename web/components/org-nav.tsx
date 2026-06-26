"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import {
  Activity,
  AlertTriangle,
  CreditCard,
  FileText,
  Gauge,
  ScrollText,
  Settings,
  Users,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { useCan } from "@/components/can";

// Org-scoped sidebar. Items are shown by role: Audit needs audit:read. Other
// reads are allowed for every role; per-action controls inside each page are
// gated separately. UI gating is convenience only — the API enforces.
export function OrgNav({ orgId }: { orgId: string }) {
  const pathname = usePathname();
  const base = `/orgs/${orgId}`;
  const canAudit = useCan("audit:read");
  const canBilling = useCan("billing:read");

  const items = [
    { href: base, label: "Overview", icon: Gauge, exact: true },
    { href: `${base}/monitors`, label: "Monitors", icon: Activity },
    { href: `${base}/incidents`, label: "Incidents", icon: AlertTriangle },
    { href: `${base}/status-pages`, label: "Status pages", icon: FileText },
    { href: `${base}/members`, label: "Members", icon: Users },
    ...(canAudit
      ? [{ href: `${base}/audit`, label: "Audit log", icon: ScrollText }]
      : []),
    ...(canBilling
      ? [{ href: `${base}/billing`, label: "Billing", icon: CreditCard }]
      : []),
    { href: `${base}/settings`, label: "Settings", icon: Settings },
  ];

  return (
    <aside className="hidden w-56 shrink-0 border-r bg-sidebar p-3 md:block">
      <nav className="space-y-1">
        {items.map(({ href, label, icon: Icon, exact }) => {
          const active = exact ? pathname === href : pathname.startsWith(href);
          return (
            <Link
              key={href}
              href={href}
              className={cn(
                "flex items-center gap-2 rounded-md px-3 py-2 text-sm font-medium transition-colors",
                active
                  ? "bg-primary text-primary-foreground"
                  : "text-muted-foreground hover:bg-muted hover:text-foreground",
              )}
            >
              <Icon className="h-4 w-4" />
              {label}
            </Link>
          );
        })}
      </nav>
    </aside>
  );
}
