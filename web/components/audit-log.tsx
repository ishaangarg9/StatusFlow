"use client";

import { useInfiniteQuery } from "@tanstack/react-query";
import { api } from "@/lib/api/client";
import { qk } from "@/lib/query-keys";
import { formatDateTime } from "@/lib/utils";
import { useCan } from "@/components/can";
import { ListError } from "@/components/list-error";
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

const PAGE_SIZE = 50;

// Read-only audit trail, newest first, keyset-paginated by the API's cursor.
// The audit:read check here only decides what to render; the API enforces it.
export function AuditLog({ orgId }: { orgId: string }) {
  const canRead = useCan("audit:read");

  const {
    data,
    isLoading,
    isError,
    refetch,
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
  } = useInfiniteQuery({
    queryKey: qk.audit(orgId),
    queryFn: ({ pageParam }) =>
      api.listAudit(orgId, { limit: PAGE_SIZE, cursor: pageParam }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.nextCursor || undefined,
    enabled: canRead,
  });

  if (!canRead) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>Audit log</CardTitle>
          <CardDescription>
            Only owners and admins can view the audit log.
          </CardDescription>
        </CardHeader>
      </Card>
    );
  }

  const entries = data?.pages.flatMap((p) => p.entries) ?? [];

  return (
    <Card>
      <CardContent className="pt-6">
        {isLoading ? (
          <Skeleton className="h-32 w-full" />
        ) : isError ? (
          <ListError message="Couldn't load the audit log." onRetry={refetch} />
        ) : entries.length === 0 ? (
          <p className="py-10 text-center text-sm text-muted-foreground">
            No activity recorded yet.
          </p>
        ) : (
          <>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>When</TableHead>
                  <TableHead>Actor</TableHead>
                  <TableHead>Action</TableHead>
                  <TableHead>Resource</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {entries.map((e) => (
                  <TableRow key={e.id}>
                    <TableCell className="whitespace-nowrap text-muted-foreground">
                      {formatDateTime(e.createdAt)}
                    </TableCell>
                    <TableCell>
                      {e.actor ?? (
                        <span className="text-muted-foreground">System</span>
                      )}
                    </TableCell>
                    <TableCell className="font-mono text-xs">{e.action}</TableCell>
                    <TableCell className="text-muted-foreground">
                      {e.resourceType ?? "—"}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
            {hasNextPage && (
              <div className="mt-4 flex justify-center">
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => fetchNextPage()}
                  disabled={isFetchingNextPage}
                >
                  {isFetchingNextPage ? "Loading…" : "Load more"}
                </Button>
              </div>
            )}
          </>
        )}
      </CardContent>
    </Card>
  );
}
