"use client";

import Link from "next/link";
import { AlertCircle } from "lucide-react";
import { Button } from "@/components/ui/button";

// Catch-all for an unexpected render/data error. The message stays generic:
// error.message from a server component is redacted in production anyway, and
// the digest is enough to find the matching server log line.
export default function Error({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  return (
    <main className="flex min-h-[60vh] flex-1 flex-col items-center justify-center gap-4 px-6 text-center">
      <AlertCircle className="h-8 w-8 text-destructive" />
      <div>
        <h1 className="text-2xl font-semibold">Something went wrong</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          Please try again. If it keeps happening, come back in a few minutes.
        </p>
        {error.digest && (
          <p className="mt-2 font-mono text-xs text-muted-foreground">
            Ref: {error.digest}
          </p>
        )}
      </div>
      <div className="flex gap-2">
        <Button onClick={() => reset()}>Try again</Button>
        <Button asChild variant="outline">
          <Link href="/orgs">Go to dashboard</Link>
        </Button>
      </div>
    </main>
  );
}
