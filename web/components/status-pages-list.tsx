"use client";

import Link from "next/link";
import { useQuery } from "@tanstack/react-query";
import { ExternalLink, Plus } from "lucide-react";
import { api } from "@/lib/api/client";
import { qk } from "@/lib/query-keys";
import { Can } from "@/components/can";
import { ListError } from "@/components/list-error";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Card,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

export function StatusPagesList({ orgId }: { orgId: string }) {
  const { data: pages, isLoading, isError, refetch } = useQuery({
    queryKey: qk.statusPages(orgId),
    queryFn: () => api.listStatusPages(orgId),
  });

  return (
    <div>
      <div className="mb-6 flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold">Status pages</h1>
          <p className="text-sm text-muted-foreground">
            Public pages that show your monitors&apos; health.
          </p>
        </div>
        <Can action="statuspage:publish">
          <Button asChild>
            <Link href={`/orgs/${orgId}/status-pages/new`}>
              <Plus className="mr-2 h-4 w-4" />
              New status page
            </Link>
          </Button>
        </Can>
      </div>

      {isLoading ? (
        <Skeleton className="h-32 w-full" />
      ) : isError ? (
        <ListError message="Couldn't load status pages." onRetry={refetch} />
      ) : pages && pages.length > 0 ? (
        <ul className="grid gap-3">
          {pages.map((p) => (
            <li key={p.id}>
              <Card>
                <CardHeader className="flex-row items-center justify-between">
                  <div>
                    <CardTitle className="text-base">
                      <Link
                        href={`/orgs/${orgId}/status-pages/${p.id}`}
                        className="hover:underline"
                      >
                        {p.title}
                      </Link>
                    </CardTitle>
                    <CardDescription>
                      /status/{p.slug} · {p.monitorIds.length} monitor
                      {p.monitorIds.length === 1 ? "" : "s"}
                    </CardDescription>
                  </div>
                  <div className="flex items-center gap-2">
                    <Badge variant={p.isPublic ? "default" : "secondary"}>
                      {p.isPublic ? "public" : "private"}
                    </Badge>
                    {p.isPublic && (
                      <Button asChild variant="ghost" size="icon">
                        <a
                          href={`/status/${p.slug}`}
                          target="_blank"
                          rel="noreferrer"
                        >
                          <ExternalLink className="h-4 w-4" />
                        </a>
                      </Button>
                    )}
                  </div>
                </CardHeader>
              </Card>
            </li>
          ))}
        </ul>
      ) : (
        <p className="text-sm text-muted-foreground">No status pages yet.</p>
      )}
    </div>
  );
}
