"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@/lib/api/client";
import { errorMessage } from "@/lib/errors";
import { qk } from "@/lib/query-keys";
import { formatDateTime } from "@/lib/utils";
import type { IncidentUpdateStatus } from "@/lib/types";
import { Can } from "@/components/can";
import { StatusBadge } from "@/components/status-badge";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

const UPDATE_STATUSES: IncidentUpdateStatus[] = [
  "investigating",
  "identified",
  "monitoring",
];

export function IncidentDetail({
  orgId,
  incidentId,
}: {
  orgId: string;
  incidentId: string;
}) {
  const router = useRouter();
  const qc = useQueryClient();
  const [message, setMessage] = useState("");
  const [status, setStatus] = useState<IncidentUpdateStatus>("investigating");

  const { data: incident, isLoading } = useQuery({
    queryKey: qk.incident(orgId, incidentId),
    queryFn: () => api.getIncident(orgId, incidentId),
    refetchInterval: 15000,
  });

  function invalidate() {
    qc.invalidateQueries({ queryKey: qk.incident(orgId, incidentId) });
    qc.invalidateQueries({ queryKey: qk.incidents(orgId) });
  }

  const addUpdate = useMutation({
    mutationFn: () =>
      api.addIncidentUpdate(orgId, incidentId, { message, status }),
    onSuccess: () => {
      toast.success("Update posted");
      setMessage("");
      invalidate();
    },
    onError: (e) => toast.error(errorMessage(e)),
  });

  const resolve = useMutation({
    mutationFn: () => api.resolveIncident(orgId, incidentId),
    onSuccess: () => {
      toast.success("Incident resolved");
      invalidate();
      router.refresh();
    },
    onError: (e) => toast.error(errorMessage(e)),
  });

  if (isLoading || !incident) {
    return <Skeleton className="h-64 w-full" />;
  }

  const isOpen = incident.status === "open";

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between">
        <div>
          <div className="flex items-center gap-3">
            <h1 className="text-2xl font-semibold">{incident.title}</h1>
            <StatusBadge status={incident.status} />
          </div>
          <p className="mt-1 text-sm text-muted-foreground">
            Started {formatDateTime(incident.startedAt)}
            {incident.resolvedAt
              ? ` · resolved ${formatDateTime(incident.resolvedAt)}`
              : ""}
          </p>
        </div>
        {isOpen && (
          <Can action="incident:resolve">
            <Button
              variant="outline"
              onClick={() => resolve.mutate()}
              disabled={resolve.isPending}
            >
              Resolve incident
            </Button>
          </Can>
        )}
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Timeline</CardTitle>
        </CardHeader>
        <CardContent>
          {incident.updates && incident.updates.length > 0 ? (
            <ol className="relative space-y-5 border-l pl-5">
              {incident.updates.map((u) => (
                <li key={u.id} className="relative">
                  <span className="absolute -left-[1.45rem] top-1 h-2.5 w-2.5 rounded-full bg-border" />
                  <div className="flex items-center gap-2">
                    <StatusBadge status={u.status} />
                    <time className="text-xs text-muted-foreground">
                      {formatDateTime(u.createdAt)}
                    </time>
                  </div>
                  <p className="mt-1 text-sm">{u.message}</p>
                </li>
              ))}
            </ol>
          ) : (
            <p className="text-sm text-muted-foreground">No updates yet.</p>
          )}
        </CardContent>
      </Card>

      {isOpen && (
        <Can action="incident:update">
          <Card>
            <CardHeader>
              <CardTitle>Post an update</CardTitle>
            </CardHeader>
            <form
              onSubmit={(e) => {
                e.preventDefault();
                addUpdate.mutate();
              }}
            >
              <CardContent className="space-y-4">
                <div className="space-y-2">
                  <Label>Status</Label>
                  <Select
                    value={status}
                    onValueChange={(v) => setStatus(v as IncidentUpdateStatus)}
                  >
                    <SelectTrigger className="w-48">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {UPDATE_STATUSES.map((s) => (
                        <SelectItem key={s} value={s} className="capitalize">
                          {s}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <div className="space-y-2">
                  <Label htmlFor="message">Message</Label>
                  <Textarea
                    id="message"
                    required
                    value={message}
                    onChange={(e) => setMessage(e.target.value)}
                    placeholder="What's the latest?"
                  />
                </div>
                <Button
                  type="submit"
                  disabled={addUpdate.isPending || !message}
                >
                  {addUpdate.isPending ? "Posting…" : "Post update"}
                </Button>
              </CardContent>
            </form>
          </Card>
        </Can>
      )}
    </div>
  );
}
