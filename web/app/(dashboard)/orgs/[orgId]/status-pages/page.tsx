import { StatusPagesList } from "@/components/status-pages-list";

export default async function StatusPagesPage({
  params,
}: {
  params: Promise<{ orgId: string }>;
}) {
  const { orgId } = await params;
  return (
    <div className="mx-auto w-full max-w-4xl px-6 py-8">
      <StatusPagesList orgId={orgId} />
    </div>
  );
}
