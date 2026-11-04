"use client";

import Link from "next/link";
import { AlertCircle } from "lucide-react";
import { ApiError } from "@/lib/errors";
import { Button } from "@/components/ui/button";

// ListError renders an inline error with a retry action for a failed data
// fetch. Without it, a query error leaves `data` undefined and the list falls
// through to its empty state — an outage would masquerade as an empty account.
export function ListError({
  message = "Couldn't load this data.",
  detail = "Something went wrong while fetching. Please try again.",
  onRetry,
  back,
}: {
  message?: string;
  detail?: string;
  onRetry?: () => void;
  back?: { href: string; label: string };
}) {
  return (
    <div className="flex flex-col items-center gap-3 py-10 text-center">
      <AlertCircle className="h-6 w-6 text-destructive" />
      <div>
        <p className="text-sm font-medium">{message}</p>
        <p className="text-sm text-muted-foreground">
          {detail}
        </p>
      </div>
      <div className="flex gap-2">
        {onRetry && (
          <Button variant="outline" size="sm" onClick={() => onRetry()}>
            Retry
          </Button>
        )}
        {back && (
          <Button asChild variant="ghost" size="sm">
            <Link href={back.href}>{back.label}</Link>
          </Button>
        )}
      </div>
    </div>
  );
}

// DetailError is the single-resource variant: a 404 (deleted, or never
// visible to this org) reads as "not found" with no retry, since retrying
// can't help; anything else offers a retry. Both link back to the list.
export function DetailError({
  error,
  noun,
  onRetry,
  back,
}: {
  error: unknown;
  noun: string;
  onRetry: () => void;
  back: { href: string; label: string };
}) {
  if (error instanceof ApiError && error.status === 404) {
    return (
      <ListError
        message={`${noun} not found.`}
        detail="It may have been deleted, or the link is wrong."
        back={back}
      />
    );
  }
  return (
    <ListError
      message={`Couldn't load this ${noun.toLowerCase()}.`}
      onRetry={onRetry}
      back={back}
    />
  );
}
