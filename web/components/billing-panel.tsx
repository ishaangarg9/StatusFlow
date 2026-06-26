"use client";

import { useEffect } from "react";
import { useSearchParams } from "next/navigation";
import { useMutation, useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Check } from "lucide-react";
import { api } from "@/lib/api/client";
import { errorMessage } from "@/lib/errors";
import { qk } from "@/lib/query-keys";
import { formatDateTime } from "@/lib/utils";
import { useCan } from "@/components/can";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

function fmtLimit(n: number): string {
  return n < 0 ? "Unlimited" : String(n);
}

export function BillingPanel({ orgId }: { orgId: string }) {
  const canRead = useCan("billing:read");
  const canManage = useCan("billing:manage");
  const params = useSearchParams();

  // Surface the Checkout return state once, on mount.
  useEffect(() => {
    const c = params.get("checkout");
    if (c === "success") toast.success("Subscription updated. Welcome to Pro!");
    else if (c === "cancelled") toast("Checkout cancelled.");
  }, [params]);

  const { data: sub, isLoading } = useQuery({
    queryKey: qk.subscription(orgId),
    queryFn: () => api.getSubscription(orgId),
    enabled: canRead,
  });

  // Both flows hand back a Stripe-hosted URL we redirect the whole window to.
  const checkout = useMutation({
    mutationFn: () => api.startCheckout(orgId),
    onSuccess: (url) => {
      window.location.href = url;
    },
    onError: (e) => toast.error(errorMessage(e)),
  });
  const portal = useMutation({
    mutationFn: () => api.startPortal(orgId),
    onSuccess: (url) => {
      window.location.href = url;
    },
    onError: (e) => toast.error(errorMessage(e)),
  });

  if (!canRead) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>Billing</CardTitle>
          <CardDescription>
            Only owners and admins can view billing for this organization.
          </CardDescription>
        </CardHeader>
      </Card>
    );
  }

  if (isLoading || !sub) {
    return <Skeleton className="h-48 w-full" />;
  }

  const isPro = sub.plan === "pro";
  const lapsed = isPro && sub.status !== "active" && sub.status !== "trialing";

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader className="flex-row items-start justify-between">
          <div>
            <CardTitle className="flex items-center gap-2">
              {isPro ? "Pro plan" : "Free plan"}
              <Badge variant={isPro ? "default" : "secondary"}>
                {sub.status}
              </Badge>
            </CardTitle>
            <CardDescription>
              {isPro
                ? "Unlimited monitors and status pages."
                : "Up to 3 monitors and 1 status page."}
              {sub.currentPeriodEnd
                ? ` Renews ${formatDateTime(sub.currentPeriodEnd)}.`
                : ""}
            </CardDescription>
          </div>
        </CardHeader>
        <CardContent className="space-y-2 text-sm">
          <div className="flex justify-between">
            <span className="text-muted-foreground">Monitors</span>
            <span>
              {sub.usage.monitors} / {fmtLimit(sub.limits.maxMonitors)}
            </span>
          </div>
          <div className="flex justify-between">
            <span className="text-muted-foreground">Status pages</span>
            <span>
              {sub.usage.statusPages} / {fmtLimit(sub.limits.maxStatusPages)}
            </span>
          </div>
          {lapsed && (
            <p className="rounded-md bg-destructive/10 p-2 text-xs text-destructive">
              Your subscription is {sub.status}; paid limits are paused until
              payment is resolved.
            </p>
          )}
        </CardContent>
        {canManage && (
          <CardFooter className="gap-2">
            {!isPro && (
              <Button
                onClick={() => checkout.mutate()}
                disabled={checkout.isPending || !sub.configured}
              >
                {checkout.isPending ? "Redirecting…" : "Upgrade to Pro"}
              </Button>
            )}
            {sub.hasCustomer && (
              <Button
                variant="outline"
                onClick={() => portal.mutate()}
                disabled={portal.isPending}
              >
                {portal.isPending ? "Redirecting…" : "Manage billing"}
              </Button>
            )}
            {!sub.configured && (
              <p className="text-xs text-muted-foreground">
                Billing is not configured on this deployment.
              </p>
            )}
          </CardFooter>
        )}
      </Card>

      {!isPro && (
        <Card>
          <CardHeader>
            <CardTitle>Why Pro?</CardTitle>
          </CardHeader>
          <CardContent>
            <ul className="space-y-2 text-sm">
              {[
                "Unlimited monitors",
                "Unlimited status pages",
                "Priority support",
              ].map((f) => (
                <li key={f} className="flex items-center gap-2">
                  <Check className="h-4 w-4 text-success" />
                  {f}
                </li>
              ))}
            </ul>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
