import { MonitorDetail } from "@/components/monitor-detail";

export default async function MonitorPage({
  params,
}: {
  params: Promise<{ orgId: string; monitorId: string }>;
}) {
  const { orgId, monitorId } = await params;
  return (
    <div className="mx-auto w-full max-w-3xl px-6 py-8">
      <MonitorDetail orgId={orgId} monitorId={monitorId} />
    </div>
  );
}
