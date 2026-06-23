import { MonitorForm } from "@/components/forms/monitor-form";

export default async function NewMonitorPage({
  params,
}: {
  params: Promise<{ orgId: string }>;
}) {
  const { orgId } = await params;
  return (
    <div className="mx-auto w-full max-w-2xl px-6 py-8">
      <MonitorForm orgId={orgId} />
    </div>
  );
}
