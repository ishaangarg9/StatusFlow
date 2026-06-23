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
import { Card, CardContent } from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

function fmt(ts: string) {
  return new Date(ts).toLocaleString();
}

export function IncidentsList({ orgId }: { orgId: string }) {
  const { data: incidents, isLoading } = useQuery({
    queryKey: qk.incidents(orgId),
    queryFn: () => api.listIncidents(orgId),
    refetchInterval: 15000,
  });

  const { data: monitors } = useQuery({
    queryKey: qk.monitors(orgId),
    queryFn: () => api.listMonitors(orgId),
  });

  const nameFor = (id: string) =>
    monitors?.find((m) => m.id === id)?.name ?? "—";

  return (
    <div>
      <div className="mb-6 flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold">Incidents</h1>
          <p className="text-sm text-muted-foreground">
            Manual and worker-opened incidents.
          </p>
        </div>
        <Can action="incident:create">
          <Button asChild>
            <Link href={`/orgs/${orgId}/incidents/new`}>
              <Plus className="mr-2 h-4 w-4" />
              New incident
            </Link>
          </Button>
        </Can>
      </div>

      <Card>
        <CardContent className="pt-6">
          {isLoading ? (
            <Skeleton className="h-32 w-full" />
          ) : incidents && incidents.length > 0 ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Title</TableHead>
                  <TableHead>Monitor</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Started</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {incidents.map((inc) => (
                  <TableRow key={inc.id}>
                    <TableCell className="font-medium">
                      <Link
                        href={`/orgs/${orgId}/incidents/${inc.id}`}
                        className="hover:underline"
                      >
                        {inc.title}
                      </Link>
                    </TableCell>
                    <TableCell className="text-muted-foreground">
                      {nameFor(inc.monitorId)}
                    </TableCell>
                    <TableCell>
                      <StatusBadge status={inc.status} />
                    </TableCell>
                    <TableCell className="text-muted-foreground">
                      {fmt(inc.startedAt)}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <p className="py-6 text-center text-sm text-muted-foreground">
              No incidents. That&apos;s a good thing.
            </p>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
