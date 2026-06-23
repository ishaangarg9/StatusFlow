"use client";

import { useRouter } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@/lib/api/client";
import { errorMessage } from "@/lib/errors";
import { qk } from "@/lib/query-keys";
import { formatDateTime } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

export function SessionsManager() {
  const router = useRouter();
  const qc = useQueryClient();

  const { data: sessions, isLoading } = useQuery({
    queryKey: qk.sessions,
    queryFn: () => api.listSessions(),
  });

  const revoke = useMutation({
    mutationFn: (id: string) => api.revokeSession(id),
    onSuccess: () => {
      toast.success("Session revoked");
      qc.invalidateQueries({ queryKey: qk.sessions });
    },
    onError: (e) => toast.error(errorMessage(e)),
  });

  const logoutAll = useMutation({
    mutationFn: () => api.logoutAll(),
    onSuccess: () => {
      toast.success("Signed out everywhere");
      router.push("/login");
      router.refresh();
    },
    onError: (e) => toast.error(errorMessage(e)),
  });

  return (
    <Card>
      <CardHeader className="flex-row items-start justify-between">
        <div>
          <CardTitle>Active sessions</CardTitle>
          <CardDescription>
            Devices currently signed in to your account.
          </CardDescription>
        </div>
        <Button
          variant="outline"
          size="sm"
          onClick={() => logoutAll.mutate()}
          disabled={logoutAll.isPending}
        >
          Sign out everywhere
        </Button>
      </CardHeader>
      <CardContent className="space-y-3">
        {isLoading && <Skeleton className="h-16 w-full" />}
        {sessions?.map((s) => (
          <div
            key={s.id}
            className="flex items-center justify-between rounded-md border p-3"
          >
            <div className="min-w-0">
              <div className="flex items-center gap-2">
                <span className="truncate text-sm font-medium">
                  {s.userAgent || "Unknown device"}
                </span>
                {s.current && <Badge variant="secondary">This device</Badge>}
              </div>
              <p className="mt-0.5 text-xs text-muted-foreground">
                {s.ip ? `${s.ip} · ` : ""}since {formatDateTime(s.createdAt)} ·
                expires {formatDateTime(s.expiresAt)}
              </p>
            </div>
            {!s.current && (
              <Button
                variant="ghost"
                size="sm"
                onClick={() => revoke.mutate(s.id)}
                disabled={revoke.isPending}
              >
                Revoke
              </Button>
            )}
          </div>
        ))}
      </CardContent>
    </Card>
  );
}
