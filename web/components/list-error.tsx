"use client";

import { AlertCircle } from "lucide-react";
import { Button } from "@/components/ui/button";

// ListError renders an inline error with a retry action for a failed data
// fetch. Without it, a query error leaves `data` undefined and the list falls
// through to its empty state — an outage would masquerade as an empty account.
export function ListError({
  message = "Couldn't load this data.",
  onRetry,
}: {
  message?: string;
  onRetry?: () => void;
}) {
  return (
    <div className="flex flex-col items-center gap-3 py-10 text-center">
      <AlertCircle className="h-6 w-6 text-destructive" />
      <div>
        <p className="text-sm font-medium">{message}</p>
        <p className="text-sm text-muted-foreground">
          Something went wrong while fetching. Please try again.
        </p>
      </div>
      {onRetry && (
        <Button variant="outline" size="sm" onClick={() => onRetry()}>
          Retry
        </Button>
      )}
    </div>
  );
}
