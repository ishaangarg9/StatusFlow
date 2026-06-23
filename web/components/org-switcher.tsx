"use client";

import { useParams, useRouter } from "next/navigation";
import { Check, ChevronsUpDown, Plus } from "lucide-react";
import type { Membership } from "@/lib/types";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";

// Switches the active org. The URL is the source of truth: selecting an org
// navigates to /orgs/<id>; the current org is read from the route params.
export function OrgSwitcher({ memberships }: { memberships: Membership[] }) {
  const router = useRouter();
  const params = useParams<{ orgId?: string }>();
  const currentId = params?.orgId;
  const current = memberships.find((m) => m.orgId === currentId);

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="sm" className="gap-1.5">
          <span className="max-w-[12rem] truncate">
            {current ? current.orgName : "Select organization"}
          </span>
          <ChevronsUpDown className="h-4 w-4 text-muted-foreground" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="w-64">
        <DropdownMenuLabel>Organizations</DropdownMenuLabel>
        <DropdownMenuSeparator />
        {memberships.length === 0 && (
          <DropdownMenuItem disabled>No organizations yet</DropdownMenuItem>
        )}
        {memberships.map((m) => (
          <DropdownMenuItem
            key={m.orgId}
            onClick={() => router.push(`/orgs/${m.orgId}`)}
          >
            <span className="truncate">{m.orgName}</span>
            <span className="ml-auto text-xs text-muted-foreground">
              {m.role}
            </span>
            {m.orgId === currentId && <Check className="ml-2 h-4 w-4" />}
          </DropdownMenuItem>
        ))}
        <DropdownMenuSeparator />
        <DropdownMenuItem onClick={() => router.push("/orgs/new")}>
          <Plus className="mr-2 h-4 w-4" />
          New organization
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
