import Link from "next/link";
import { serverApi } from "@/lib/api/server";
import { StatusBadge } from "@/components/status-badge";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

export default async function OrgOverview({
  params,
}: {
  params: Promise<{ orgId: string }>;
}) {
  const { orgId } = await params;
  const [org, monitors, incidents] = await Promise.all([
    serverApi.getOrg(orgId),
    serverApi.listMonitors(orgId),
    serverApi.listIncidents(orgId),
  ]);

  const active = monitors.filter((m) => !m.isPaused).length;
  const open = incidents.filter((i) => i.status === "open");

  const stats = [
    { label: "Monitors", value: monitors.length, href: `/orgs/${orgId}/monitors` },
    { label: "Active", value: active, href: `/orgs/${orgId}/monitors` },
    { label: "Open incidents", value: open.length, href: `/orgs/${orgId}/incidents` },
  ];

  return (
    <div className="mx-auto w-full max-w-5xl space-y-8 px-6 py-8">
      <div>
        <h1 className="text-2xl font-semibold">{org.name}</h1>
        <p className="mt-1 text-sm text-muted-foreground">/{org.slug}</p>
      </div>

      <div className="grid gap-4 sm:grid-cols-3">
        {stats.map((s) => (
          <Link key={s.label} href={s.href}>
            <Card className="transition-colors hover:bg-muted/50">
              <CardHeader className="pb-2">
                <CardDescription>{s.label}</CardDescription>
                <CardTitle className="text-3xl">{s.value}</CardTitle>
              </CardHeader>
            </Card>
          </Link>
        ))}
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Open incidents</CardTitle>
          <CardDescription>
            Active incidents across this organization.
          </CardDescription>
        </CardHeader>
        <CardContent>
          {open.length > 0 ? (
            <ul className="divide-y">
              {open.map((i) => (
                <li key={i.id} className="flex items-center justify-between py-3">
                  <Link
                    href={`/orgs/${orgId}/incidents/${i.id}`}
                    className="font-medium hover:underline"
                  >
                    {i.title}
                  </Link>
                  <StatusBadge status={i.status} />
                </li>
              ))}
            </ul>
          ) : (
            <p className="text-sm text-muted-foreground">
              All systems operational.
            </p>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
