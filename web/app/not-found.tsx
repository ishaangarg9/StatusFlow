import Link from "next/link";
import { Activity } from "lucide-react";
import { Button } from "@/components/ui/button";

// Shared 404, also what an org you're not a member of renders (the [orgId]
// layout calls notFound() so a non-member can't tell the org exists).
export default function NotFound() {
  return (
    <main className="flex min-h-screen flex-col items-center justify-center gap-4 px-6 text-center">
      <Link href="/" className="flex items-center gap-2 font-semibold">
        <Activity className="h-5 w-5" />
        StatusFlow
      </Link>
      <div>
        <h1 className="text-2xl font-semibold">Page not found</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          The page you&apos;re looking for doesn&apos;t exist or you don&apos;t
          have access to it.
        </p>
      </div>
      <div className="flex gap-2">
        <Button asChild>
          <Link href="/orgs">Go to dashboard</Link>
        </Button>
        <Button asChild variant="outline">
          <Link href="/">Home</Link>
        </Button>
      </div>
    </main>
  );
}
