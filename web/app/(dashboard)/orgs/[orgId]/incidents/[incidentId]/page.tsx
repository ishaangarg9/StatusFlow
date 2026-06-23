import { IncidentDetail } from "@/components/incident-detail";

export default async function IncidentPage({
  params,
}: {
  params: Promise<{ orgId: string; incidentId: string }>;
}) {
  const { orgId, incidentId } = await params;
  return (
    <div className="mx-auto w-full max-w-3xl px-6 py-8">
      <IncidentDetail orgId={orgId} incidentId={incidentId} />
    </div>
  );
}
