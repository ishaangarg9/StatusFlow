"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@/lib/api/client";
import { errorMessage } from "@/lib/errors";
import { qk } from "@/lib/query-keys";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
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

export function IncidentForm({ orgId }: { orgId: string }) {
  const router = useRouter();
  const qc = useQueryClient();
  const [monitorId, setMonitorId] = useState("");
  const [title, setTitle] = useState("");

  const { data: monitors } = useQuery({
    queryKey: qk.monitors(orgId),
    queryFn: () => api.listMonitors(orgId),
  });

  const create = useMutation({
    mutationFn: () => api.createIncident(orgId, { monitorId, title }),
    onSuccess: (inc) => {
      toast.success("Incident opened");
      qc.invalidateQueries({ queryKey: qk.incidents(orgId) });
      router.push(`/orgs/${orgId}/incidents/${inc.id}`);
      router.refresh();
    },
    onError: (e) => toast.error(errorMessage(e)),
  });

  return (
    <Card>
      <CardHeader>
        <CardTitle>New incident</CardTitle>
      </CardHeader>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          create.mutate();
        }}
      >
        <CardContent className="space-y-4">
          <div className="space-y-2">
            <Label>Monitor</Label>
            <Select value={monitorId} onValueChange={setMonitorId}>
              <SelectTrigger>
                <SelectValue placeholder="Select a monitor" />
              </SelectTrigger>
              <SelectContent>
                {monitors?.map((m) => (
                  <SelectItem key={m.id} value={m.id}>
                    {m.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-2">
            <Label htmlFor="title">Title</Label>
            <Input
              id="title"
              required
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              placeholder="Elevated error rates"
            />
          </div>
        </CardContent>
        <CardFooter className="mt-4 gap-2">
          <Button
            type="submit"
            disabled={create.isPending || !monitorId || !title}
          >
            {create.isPending ? "Opening…" : "Open incident"}
          </Button>
          <Button type="button" variant="ghost" onClick={() => router.back()}>
            Cancel
          </Button>
        </CardFooter>
      </form>
    </Card>
  );
}
