"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@/lib/api/client";
import { qk } from "@/lib/query-keys";
import type { Org } from "@/lib/types";
import { useCan } from "@/components/can";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

export function OrgSettings({ org }: { org: Org }) {
  const router = useRouter();
  const qc = useQueryClient();
  const canUpdate = useCan("org:update");
  const canDelete = useCan("org:delete");

  const [name, setName] = useState(org.name);
  const [slug, setSlug] = useState(org.slug);

  const save = useMutation({
    mutationFn: () => api.updateOrg(org.id, { name, slug }),
    onSuccess: () => {
      toast.success("Organization updated");
      qc.invalidateQueries({ queryKey: qk.org(org.id) });
      qc.invalidateQueries({ queryKey: qk.me });
      router.refresh();
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : "Failed"),
  });

  const remove = useMutation({
    mutationFn: () => api.deleteOrg(org.id),
    onSuccess: () => {
      toast.success("Organization deleted");
      router.push("/orgs");
      router.refresh();
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : "Failed"),
  });

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader>
          <CardTitle>General</CardTitle>
          <CardDescription>Organization name and URL slug.</CardDescription>
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
                value={name}
                onChange={(e) => setName(e.target.value)}
                disabled={!canUpdate}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="slug">Slug</Label>
              <Input
                id="slug"
                value={slug}
                onChange={(e) => setSlug(e.target.value)}
                disabled={!canUpdate}
              />
            </div>
          </CardContent>
          {canUpdate && (
            <CardFooter className="mt-4">
              <Button type="submit" disabled={save.isPending}>
                {save.isPending ? "Saving…" : "Save changes"}
              </Button>
            </CardFooter>
          )}
        </form>
      </Card>

      {canDelete && (
        <Card className="border-destructive/50">
          <CardHeader>
            <CardTitle>Danger zone</CardTitle>
            <CardDescription>
              Deleting an organization removes all of its monitors, incidents,
              and status pages. This cannot be undone.
            </CardDescription>
          </CardHeader>
          <CardContent>
            <ConfirmDialog
              title="Delete this organization?"
              description={`"${org.name}" and all of its data will be permanently deleted.`}
              confirmLabel="Delete organization"
              onConfirm={() => remove.mutateAsync()}
              trigger={<Button variant="destructive">Delete organization</Button>}
            />
          </CardContent>
        </Card>
      )}
    </div>
  );
}
