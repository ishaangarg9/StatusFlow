"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@/lib/api/client";
import type { MonitorInput } from "@/lib/api/endpoints";
import { errorMessage } from "@/lib/errors";
import { qk } from "@/lib/query-keys";
import type { HttpMethod, Monitor } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import {
  Card,
  CardContent,
  CardFooter,
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

const METHODS: HttpMethod[] = ["GET", "HEAD", "POST"];

export function MonitorForm({
  orgId,
  monitor,
}: {
  orgId: string;
  monitor?: Monitor;
}) {
  const router = useRouter();
  const qc = useQueryClient();
  const editing = Boolean(monitor);

  const [name, setName] = useState(monitor?.name ?? "");
  const [url, setUrl] = useState(monitor?.url ?? "https://");
  const [method, setMethod] = useState<HttpMethod>(monitor?.method ?? "GET");
  const [expectedStatus, setExpectedStatus] = useState(
    monitor?.expectedStatus ?? 200,
  );
  const [intervalSeconds, setIntervalSeconds] = useState(
    monitor?.intervalSeconds ?? 60,
  );
  const [timeoutMs, setTimeoutMs] = useState(monitor?.timeoutMs ?? 10000);
  const [isPaused, setIsPaused] = useState(monitor?.isPaused ?? false);

  const save = useMutation({
    mutationFn: () => {
      const body: MonitorInput = {
        name,
        url,
        method,
        expectedStatus,
        intervalSeconds,
        timeoutMs,
        isPaused,
      };
      return editing
        ? api.updateMonitor(orgId, monitor!.id, body)
        : api.createMonitor(orgId, body);
    },
    onSuccess: (m) => {
      toast.success(editing ? "Monitor updated" : "Monitor created");
      qc.invalidateQueries({ queryKey: qk.monitors(orgId) });
      qc.invalidateQueries({ queryKey: qk.monitor(orgId, m.id) });
      router.push(`/orgs/${orgId}/monitors/${m.id}`);
      router.refresh();
    },
    onError: (e) => toast.error(errorMessage(e)),
  });

  return (
    <Card>
      <CardHeader>
        <CardTitle>{editing ? "Edit monitor" : "New monitor"}</CardTitle>
      </CardHeader>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate();
        }}
      >
        <CardContent className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="name">Name</Label>
            <Input
              id="name"
              required
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Marketing site"
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="url">URL</Label>
            <Input
              id="url"
              type="url"
              required
              value={url}
              onChange={(e) => setUrl(e.target.value)}
            />
          </div>
          <div className="grid grid-cols-2 gap-4">
            <div className="space-y-2">
              <Label>Method</Label>
              <Select
                value={method}
                onValueChange={(v) => setMethod(v as HttpMethod)}
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {METHODS.map((m) => (
                    <SelectItem key={m} value={m}>
                      {m}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-2">
              <Label htmlFor="expectedStatus">Expected status</Label>
              <Input
                id="expectedStatus"
                type="number"
                min={100}
                max={599}
                value={expectedStatus}
                onChange={(e) => setExpectedStatus(Number(e.target.value))}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="interval">Interval (seconds)</Label>
              <Input
                id="interval"
                type="number"
                min={30}
                value={intervalSeconds}
                onChange={(e) => setIntervalSeconds(Number(e.target.value))}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="timeout">Timeout (ms)</Label>
              <Input
                id="timeout"
                type="number"
                min={1000}
                max={60000}
                value={timeoutMs}
                onChange={(e) => setTimeoutMs(Number(e.target.value))}
              />
            </div>
          </div>
          {editing && (
            <div className="flex items-center justify-between rounded-md border p-3">
              <div>
                <Label htmlFor="paused">Paused</Label>
                <p className="text-xs text-muted-foreground">
                  Pause checks without deleting the monitor.
                </p>
              </div>
              <Switch
                id="paused"
                checked={isPaused}
                onCheckedChange={setIsPaused}
              />
            </div>
          )}
        </CardContent>
        <CardFooter className="mt-4 gap-2">
          <Button type="submit" disabled={save.isPending || !name}>
            {save.isPending ? "Saving…" : editing ? "Save changes" : "Create monitor"}
          </Button>
          <Button
            type="button"
            variant="ghost"
            onClick={() => router.back()}
          >
            Cancel
          </Button>
        </CardFooter>
      </form>
    </Card>
  );
}
