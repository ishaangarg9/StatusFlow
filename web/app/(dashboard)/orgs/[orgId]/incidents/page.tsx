import { IncidentsList } from "@/components/incidents-list";

export default async function IncidentsPage({
  params,
}: {
  params: Promise<{ orgId: string }>;
}) {
  const { orgId } = await params;
  return (
    <div className="mx-auto w-full max-w-5xl px-6 py-8">
      <IncidentsList orgId={orgId} />
    </div>
  );
}
