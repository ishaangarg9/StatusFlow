"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@/lib/api/client";
import { errorMessage } from "@/lib/errors";
import { qk } from "@/lib/query-keys";
import { MonitorPicker } from "@/components/monitor-picker";
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

function slugify(s: string) {
  return s
    .toLowerCase()
    .trim()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

export function StatusPageForm({ orgId }: { orgId: string }) {
  const router = useRouter();
  const qc = useQueryClient();
  const [title, setTitle] = useState("");
  const [slug, setSlug] = useState("");
  const [slugEdited, setSlugEdited] = useState(false);
  const [isPublic, setIsPublic] = useState(true);
  const [monitorIds, setMonitorIds] = useState<string[]>([]);

  const effectiveSlug = slugEdited ? slug : slugify(title);

  const create = useMutation({
    mutationFn: () =>
      api.createStatusPage(orgId, {
        title,
        slug: effectiveSlug,
        isPublic,
        monitorIds,
      }),
    onSuccess: (page) => {
      toast.success("Status page created");
      qc.invalidateQueries({ queryKey: qk.statusPages(orgId) });
      router.push(`/orgs/${orgId}/status-pages/${page.id}`);
      router.refresh();
    },
    onError: (e) => toast.error(errorMessage(e)),
  });

  return (
    <Card>
      <CardHeader>
        <CardTitle>New status page</CardTitle>
      </CardHeader>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          create.mutate();
        }}
      >
        <CardContent className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="title">Title</Label>
            <Input
              id="title"
              required
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              placeholder="Acme Status"
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="slug">Slug</Label>
            <Input
              id="slug"
              required
              value={effectiveSlug}
              onChange={(e) => {
                setSlugEdited(true);
                setSlug(e.target.value);
              }}
            />
            <p className="text-xs text-muted-foreground">
              Public URL: /status/{effectiveSlug || "…"}
            </p>
          </div>
          <div className="flex items-center justify-between rounded-md border p-3">
            <div>
              <Label htmlFor="public">Public</Label>
              <p className="text-xs text-muted-foreground">
                Make this page visible at its public URL.
              </p>
            </div>
            <Switch id="public" checked={isPublic} onCheckedChange={setIsPublic} />
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
        <CardFooter className="mt-4 gap-2">
          <Button type="submit" disabled={create.isPending || !title}>
            {create.isPending ? "Creating…" : "Create status page"}
          </Button>
          <Button type="button" variant="ghost" onClick={() => router.back()}>
            Cancel
          </Button>
        </CardFooter>
      </form>
    </Card>
  );
}
