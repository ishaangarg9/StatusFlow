import { StatusPageEditor } from "@/components/status-page-editor";

export default async function StatusPageEditorPage({
  params,
}: {
  params: Promise<{ orgId: string; statusPageId: string }>;
}) {
  const { orgId, statusPageId } = await params;
  return (
    <div className="mx-auto w-full max-w-2xl px-6 py-8">
      <StatusPageEditor orgId={orgId} statusPageId={statusPageId} />
    </div>
  );
}
