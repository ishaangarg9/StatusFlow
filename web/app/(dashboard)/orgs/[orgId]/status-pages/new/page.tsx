import { StatusPageForm } from "@/components/forms/status-page-form";

export default async function NewStatusPagePage({
  params,
}: {
  params: Promise<{ orgId: string }>;
}) {
  const { orgId } = await params;
  return (
    <div className="mx-auto w-full max-w-2xl px-6 py-8">
      <StatusPageForm orgId={orgId} />
    </div>
  );
}
