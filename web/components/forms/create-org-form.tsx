"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { toast } from "sonner";
import { api } from "@/lib/api/client";
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

function slugify(s: string) {
  return s
    .toLowerCase()
    .trim()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

export function CreateOrgForm() {
  const router = useRouter();
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const [slugEdited, setSlugEdited] = useState(false);
  const [pending, setPending] = useState(false);

  const effectiveSlug = slugEdited ? slug : slugify(name);

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setPending(true);
    try {
      const org = await api.createOrg({ name, slug: effectiveSlug });
      toast.success("Organization created");
      router.push(`/orgs/${org.id}`);
      router.refresh();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Could not create org");
      setPending(false);
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>New organization</CardTitle>
        <CardDescription>
          You&apos;ll be enrolled as the owner.
        </CardDescription>
      </CardHeader>
      <form onSubmit={onSubmit}>
        <CardContent className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="name">Name</Label>
            <Input
              id="name"
              required
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Acme Inc"
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
              placeholder="acme"
            />
            <p className="text-xs text-muted-foreground">
              Used in URLs. Lowercase letters, numbers, and dashes.
            </p>
          </div>
        </CardContent>
        <CardFooter className="mt-4">
          <Button type="submit" disabled={pending || !name}>
            {pending ? "Creating…" : "Create organization"}
          </Button>
        </CardFooter>
      </form>
    </Card>
  );
}
