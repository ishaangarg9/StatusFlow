import { IncidentForm } from "@/components/forms/incident-form";

export default async function NewIncidentPage({
  params,
}: {
  params: Promise<{ orgId: string }>;
}) {
  const { orgId } = await params;
  return (
    <div className="mx-auto w-full max-w-2xl px-6 py-8">
      <IncidentForm orgId={orgId} />
    </div>
  );
}
