// Public status page. Server component (SSR / ISR). MUST NOT touch the
// session cookie. Calls the Go public endpoint and renders the strict
// projection only (no urls, no member info, no audit log).
//
// See doc 04 §8 for the response shape and doc 06 ADR-07 for why this
// surface is deliberately separate from the authenticated dashboard.

import { getPublicStatus, type PublicStatus } from "@/lib/api";
import { notFound } from "next/navigation";

export const revalidate = 30; // ISR: regenerate at most every 30s

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
  return (
    <main className="mx-auto max-w-3xl px-6 py-12">
      <header className="border-b border-neutral-200 pb-6 dark:border-neutral-800">
        <h1 className="text-2xl font-semibold">{data.title}</h1>
        <p className="mt-1 text-sm uppercase tracking-wide text-neutral-500">
          Overall: {data.overall}
        </p>
      </header>

      <section className="mt-8">
        <h2 className="text-lg font-medium">Components</h2>
        <ul className="mt-4 divide-y divide-neutral-200 dark:divide-neutral-800">
          {data.components.map((c) => (
            <li
              key={c.name}
              className="flex items-center justify-between py-3"
            >
              <span>{c.name}</span>
              <span className="text-sm text-neutral-500">{c.status}</span>
            </li>
          ))}
        </ul>
      </section>

      {data.incidents.length > 0 && (
        <section className="mt-10">
          <h2 className="text-lg font-medium">Incidents</h2>
          <ul className="mt-4 space-y-6">
            {data.incidents.map((i, idx) => (
              <li
                key={idx}
                className="rounded-lg border border-neutral-200 p-4 dark:border-neutral-800"
              >
                <div className="flex items-center justify-between">
                  <h3 className="font-medium">{i.title}</h3>
                  <span className="text-xs uppercase tracking-wide text-neutral-500">
                    {i.status}
                  </span>
                </div>
                <ul className="mt-3 space-y-2 text-sm text-neutral-600 dark:text-neutral-400">
                  {i.updates.map((u, j) => (
                    <li key={j}>
                      <time className="mr-2 text-neutral-500">
                        {u.createdAt}
                      </time>
                      <span className="mr-1 uppercase">{u.status}:</span>
                      {u.message}
                    </li>
                  ))}
                </ul>
              </li>
            ))}
          </ul>
        </section>
      )}
    </main>
  );
}
