"use client";

import { useRouter } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Trash2 } from "lucide-react";
import { api } from "@/lib/api/client";
import { qk } from "@/lib/query-keys";
import { Can } from "@/components/can";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { MonitorForm } from "@/components/forms/monitor-form";
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

function fmt(ts: string) {
  return new Date(ts).toLocaleString();
}

export function MonitorDetail({
  orgId,
  monitorId,
}: {
  orgId: string;
  monitorId: string;
}) {
  const router = useRouter();
  const qc = useQueryClient();

  const { data: monitor, isLoading } = useQuery({
    queryKey: qk.monitor(orgId, monitorId),
    queryFn: () => api.getMonitor(orgId, monitorId),
  });

  const { data: checks } = useQuery({
    queryKey: qk.checks(orgId, monitorId),
    queryFn: () => api.listChecks(orgId, monitorId, 50),
    refetchInterval: 15000,
  });

  const remove = useMutation({
    mutationFn: () => api.deleteMonitor(orgId, monitorId),
    onSuccess: () => {
      toast.success("Monitor deleted");
      qc.invalidateQueries({ queryKey: qk.monitors(orgId) });
      router.push(`/orgs/${orgId}/monitors`);
      router.refresh();
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : "Failed"),
  });

  if (isLoading || !monitor) {
    return <Skeleton className="h-64 w-full" />;
  }

  const latest = checks?.[0];

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <div className="flex items-center gap-3">
            <h1 className="text-2xl font-semibold">{monitor.name}</h1>
            {latest && <StatusBadge status={latest.status} />}
            {monitor.isPaused && <StatusBadge status="paused" />}
          </div>
          <p className="mt-1 text-sm text-muted-foreground">{monitor.url}</p>
        </div>
        <Can action="monitor:delete">
          <ConfirmDialog
            title="Delete monitor?"
            description={`"${monitor.name}" and its check history will be removed.`}
            confirmLabel="Delete"
            onConfirm={() => remove.mutateAsync()}
            trigger={
              <Button variant="outline" size="sm">
                <Trash2 className="mr-2 h-4 w-4" />
                Delete
              </Button>
            }
          />
        </Can>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Recent checks</CardTitle>
          <CardDescription>
            Results from the worker, newest first (auto-refreshes).
          </CardDescription>
        </CardHeader>
        <CardContent>
          {checks && checks.length > 0 ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Status</TableHead>
                  <TableHead>Code</TableHead>
                  <TableHead>Latency</TableHead>
                  <TableHead>Checked at</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {checks.map((c, i) => (
                  <TableRow key={i}>
                    <TableCell>
                      <StatusBadge status={c.status} />
                    </TableCell>
                    <TableCell>{c.statusCode ?? "—"}</TableCell>
                    <TableCell>
                      {c.latencyMs != null ? `${c.latencyMs} ms` : "—"}
                    </TableCell>
                    <TableCell className="text-muted-foreground">
                      {fmt(c.checkedAt)}
                      {c.error ? ` · ${c.error}` : ""}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <p className="text-sm text-muted-foreground">
              No checks recorded yet. The worker records results on the next
              interval.
            </p>
          )}
        </CardContent>
      </Card>

      <Can action="monitor:update">
        <MonitorForm orgId={orgId} monitor={monitor} />
      </Can>
    </div>
  );
}
