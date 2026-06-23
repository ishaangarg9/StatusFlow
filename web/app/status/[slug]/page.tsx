// Public status page. Server component (SSR / ISR). MUST NOT touch the
// session cookie. Calls the Go public endpoint and renders the strict
// projection only (no urls, no member info, no audit log).
//
// See doc 04 §8 for the response shape and doc 06 ADR-07 for why this
// surface is deliberately separate from the authenticated dashboard.

import { notFound } from "next/navigation";
import { CheckCircle2, AlertTriangle, XCircle } from "lucide-react";
import { getPublicStatus, type PublicStatus } from "@/lib/api";
import { StatusBadge } from "@/components/status-badge";

export const revalidate = 30; // ISR: regenerate at most every 30s

const OVERALL: Record<
  PublicStatus["overall"],
  { label: string; Icon: typeof CheckCircle2; className: string }
> = {
  operational: {
    label: "All systems operational",
    Icon: CheckCircle2,
    className: "text-success",
  },
  degraded: {
    label: "Degraded performance",
    Icon: AlertTriangle,
    className: "text-warning",
  },
  down: {
    label: "Major outage",
    Icon: XCircle,
    className: "text-destructive",
  },
};

function fmt(ts: string) {
  return new Date(ts).toLocaleString();
}

export default async function PublicStatusPage({
  params,
}: {
  params: Promise<{ slug: string }>;
}) {
  const { slug } = await params;
  const data = await getPublicStatus(slug);
  if (!data) notFound();
  return <StatusView data={data} />;
}

function StatusView({ data }: { data: PublicStatus }) {
  const overall = OVERALL[data.overall] ?? OVERALL.operational;
  const { Icon } = overall;

  return (
    <main className="mx-auto max-w-3xl px-6 py-12">
      <header>
        <h1 className="text-2xl font-semibold">{data.title}</h1>
        <div className={`mt-3 flex items-center gap-2 ${overall.className}`}>
          <Icon className="h-5 w-5" />
          <span className="text-lg font-medium">{overall.label}</span>
        </div>
      </header>

      <section className="mt-8">
        <h2 className="text-sm font-medium uppercase tracking-wide text-muted-foreground">
          Components
        </h2>
        <ul className="mt-3 divide-y rounded-lg border">
          {data.components.map((c) => (
            <li
              key={c.name}
              className="flex items-center justify-between px-4 py-3"
            >
              <span className="font-medium">{c.name}</span>
              <StatusBadge status={c.status} />
            </li>
          ))}
        </ul>
      </section>

      {data.incidents.length > 0 && (
        <section className="mt-10">
          <h2 className="text-sm font-medium uppercase tracking-wide text-muted-foreground">
            Incidents
          </h2>
          <ul className="mt-3 space-y-4">
            {data.incidents.map((i, idx) => (
              <li key={idx} className="rounded-lg border p-4">
                <div className="flex items-center justify-between">
                  <h3 className="font-medium">{i.title}</h3>
                  <StatusBadge status={i.status} />
                </div>
                <ul className="mt-3 space-y-2 border-l pl-4 text-sm">
                  {i.updates.map((u, j) => (
                    <li key={j}>
                      <div className="flex items-center gap-2">
                        <StatusBadge status={u.status} />
                        <time className="text-xs text-muted-foreground">
                          {fmt(u.createdAt)}
                        </time>
                      </div>
                      <p className="mt-1 text-muted-foreground">{u.message}</p>
                    </li>
                  ))}
                </ul>
              </li>
            ))}
          </ul>
        </section>
      )}

      <footer className="mt-12 border-t pt-6 text-center text-xs text-muted-foreground">
        Powered by StatusFlow
      </footer>
    </main>
  );
}
