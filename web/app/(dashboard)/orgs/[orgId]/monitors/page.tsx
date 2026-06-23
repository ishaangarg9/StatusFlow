import { MonitorsList } from "@/components/monitors-list";

export default async function MonitorsPage({
  params,
}: {
  params: Promise<{ orgId: string }>;
}) {
  const { orgId } = await params;
  return (
    <div className="mx-auto w-full max-w-5xl px-6 py-8">
      <MonitorsList orgId={orgId} />
    </div>
  );
}
