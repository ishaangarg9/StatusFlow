"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { ExternalLink } from "lucide-react";
import { api } from "@/lib/api/client";
import { qk } from "@/lib/query-keys";
import { useCan } from "@/components/can";
import { MonitorPicker } from "@/components/monitor-picker";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

// There is no GET-one status-page endpoint; resolve it from the list.
export function StatusPageEditor({
  orgId,
  statusPageId,
}: {
  orgId: string;
  statusPageId: string;
}) {
  const router = useRouter();
  const qc = useQueryClient();
  const canPublish = useCan("statuspage:publish");

  const { data: pages, isLoading } = useQuery({
    queryKey: qk.statusPages(orgId),
    queryFn: () => api.listStatusPages(orgId),
  });
  const page = pages?.find((p) => p.id === statusPageId);

  const [title, setTitle] = useState("");
  const [isPublic, setIsPublic] = useState(false);
  const [monitorIds, setMonitorIds] = useState<string[]>([]);

  // Seed local form state once the page is loaded.
  useEffect(() => {
    if (page) {
      setTitle(page.title);
      setIsPublic(page.isPublic);
      setMonitorIds(page.monitorIds);
    }
  }, [page]);

  const save = useMutation({
    mutationFn: async () => {
      await api.updateStatusPage(orgId, statusPageId, { title, isPublic });
      await api.setStatusPageMonitors(orgId, statusPageId, monitorIds);
    },
    onSuccess: () => {
      toast.success("Status page saved");
      qc.invalidateQueries({ queryKey: qk.statusPages(orgId) });
      router.refresh();
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : "Failed"),
  });

  if (isLoading) return <Skeleton className="h-64 w-full" />;
  if (!page) {
    return (
      <p className="text-sm text-muted-foreground">Status page not found.</p>
    );
  }

  return (
    <Card>
      <CardHeader className="flex-row items-start justify-between">
        <div>
          <CardTitle>{page.title}</CardTitle>
          <CardDescription>
            Public URL: /status/{page.slug}
          </CardDescription>
        </div>
        {page.isPublic && (
          <Button asChild variant="outline" size="sm">
            <a href={`/status/${page.slug}`} target="_blank" rel="noreferrer">
              <ExternalLink className="mr-2 h-4 w-4" />
              View
            </a>
          </Button>
        )}
      </CardHeader>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate();
        }}
      >
        <CardContent className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="title">Title</Label>
            <Input
              id="title"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              disabled={!canPublish}
            />
          </div>
          <div className="flex items-center justify-between rounded-md border p-3">
            <div>
              <Label htmlFor="public">Public</Label>
              <p className="text-xs text-muted-foreground">
                When off, the public URL returns 404.
              </p>
            </div>
            <Switch
              id="public"
              checked={isPublic}
              onCheckedChange={setIsPublic}
              disabled={!canPublish}
            />
          </div>
          <div className="space-y-2">
            <Label>Monitors</Label>
            <MonitorPicker
              orgId={orgId}
              selected={monitorIds}
              onChange={setMonitorIds}
            />
          </div>
        </CardContent>
        {canPublish && (
          <CardFooter className="mt-4">
            <Button type="submit" disabled={save.isPending}>
              {save.isPending ? "Saving…" : "Save changes"}
            </Button>
          </CardFooter>
        )}
      </form>
    </Card>
  );
}
