import Link from "next/link";
import { redirect } from "next/navigation";
import { Plus } from "lucide-react";
import { getMe } from "@/lib/auth";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";

// Org picker. The middleware guarantees a session cookie; the dashboard layout
// guarantees a valid /me, so we can read memberships here directly.
export default async function OrgsPage() {
  const me = await getMe();
  if (!me) redirect("/login");

  return (
    <main className="mx-auto w-full max-w-3xl px-6 py-10">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold">Your organizations</h1>
          <p className="text-sm text-muted-foreground">
            Pick an organization to manage, or create a new one.
          </p>
        </div>
        <Button asChild>
          <Link href="/orgs/new">
            <Plus className="mr-2 h-4 w-4" />
            New
          </Link>
        </Button>
      </div>

      {me.memberships.length === 0 ? (
        <Card className="mt-8">
          <CardHeader>
            <CardTitle>No organizations yet</CardTitle>
            <CardDescription>
              Create your first organization to start adding monitors.
            </CardDescription>
          </CardHeader>
        </Card>
      ) : (
        <ul className="mt-6 grid gap-3">
          {me.memberships.map((m) => (
            <li key={m.orgId}>
              <Link href={`/orgs/${m.orgId}`}>
                <Card className="transition-colors hover:bg-muted/50">
                  <CardHeader className="flex-row items-center justify-between">
                    <div>
                      <CardTitle className="text-base">{m.orgName}</CardTitle>
                      <CardDescription>/{m.orgSlug}</CardDescription>
                    </div>
                    <Badge variant="secondary">{m.role}</Badge>
                  </CardHeader>
                </Card>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </main>
  );
}
