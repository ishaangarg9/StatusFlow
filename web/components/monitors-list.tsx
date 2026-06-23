"use client";

import Link from "next/link";
import { useQuery } from "@tanstack/react-query";
import { Plus } from "lucide-react";
import { api } from "@/lib/api/client";
import { qk } from "@/lib/query-keys";
import { Can } from "@/components/can";
import { StatusBadge } from "@/components/status-badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

export function MonitorsList({ orgId }: { orgId: string }) {
  const { data: monitors, isLoading } = useQuery({
    queryKey: qk.monitors(orgId),
    queryFn: () => api.listMonitors(orgId),
    refetchInterval: 15000,
  });

  return (
    <div>
      <div className="mb-6 flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold">Monitors</h1>
          <p className="text-sm text-muted-foreground">
            HTTP checks running on a schedule.
          </p>
        </div>
        <Can action="monitor:create">
          <Button asChild>
            <Link href={`/orgs/${orgId}/monitors/new`}>
              <Plus className="mr-2 h-4 w-4" />
              New monitor
            </Link>
          </Button>
        </Can>
      </div>

      <Card>
        <CardContent className="pt-6">
          {isLoading ? (
            <Skeleton className="h-32 w-full" />
          ) : monitors && monitors.length > 0 ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>URL</TableHead>
                  <TableHead>Method</TableHead>
                  <TableHead>Interval</TableHead>
                  <TableHead>State</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {monitors.map((m) => (
                  <TableRow key={m.id} className="cursor-pointer">
                    <TableCell className="font-medium">
                      <Link
                        href={`/orgs/${orgId}/monitors/${m.id}`}
                        className="hover:underline"
                      >
                        {m.name}
                      </Link>
                    </TableCell>
                    <TableCell className="max-w-[18rem] truncate text-muted-foreground">
                      {m.url}
                    </TableCell>
                    <TableCell>{m.method}</TableCell>
                    <TableCell>{m.intervalSeconds}s</TableCell>
                    <TableCell>
                      <StatusBadge status={m.isPaused ? "paused" : "operational"} />
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <EmptyState orgId={orgId} />
          )}
        </CardContent>
      </Card>
    </div>
  );
}

function EmptyState({ orgId }: { orgId: string }) {
  return (
    <div className="py-10 text-center">
      <CardHeader>
        <CardTitle className="text-base">No monitors yet</CardTitle>
        <CardDescription>
          Create a monitor to start checking an endpoint.
        </CardDescription>
      </CardHeader>
      <Can action="monitor:create">
        <Button asChild className="mt-2">
          <Link href={`/orgs/${orgId}/monitors/new`}>
            <Plus className="mr-2 h-4 w-4" />
            New monitor
          </Link>
        </Button>
      </Can>
    </div>
  );
}
