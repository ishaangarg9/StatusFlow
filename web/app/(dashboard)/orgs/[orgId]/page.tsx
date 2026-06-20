// Authenticated dashboard root for a single org. Client-side fetches go
// through lib/api.ts (which targets the BFF route handlers or the Go API
// directly, per doc 06 ADR-11). Permission enforcement is server-side; this
// UI only hides controls a viewer can't use.

export default async function OrgDashboard({
  params,
}: {
  params: Promise<{ orgId: string }>;
}) {
  const { orgId } = await params;
  return (
    <main className="mx-auto max-w-5xl px-6 py-10">
      <h1 className="text-2xl font-semibold">Org dashboard</h1>
      <p className="mt-1 text-sm text-neutral-500">org_id: {orgId}</p>
      <p className="mt-6 text-neutral-600 dark:text-neutral-400">
        Monitors, incidents, and members appear here once the API endpoints
        are wired up. See doc 04 for the route table.
      </p>
    </main>
  );
}
