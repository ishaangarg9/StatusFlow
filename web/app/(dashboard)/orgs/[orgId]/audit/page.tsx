import { AuditLog } from "@/components/audit-log";

export default async function AuditPage({
  params,
}: {
  params: Promise<{ orgId: string }>;
}) {
  const { orgId } = await params;
  return (
    <div className="mx-auto w-full max-w-5xl px-6 py-8">
      <div className="mb-6">
        <h1 className="text-2xl font-semibold">Audit log</h1>
        <p className="text-sm text-muted-foreground">
          Every change made in this organization, newest first.
        </p>
      </div>
      <AuditLog orgId={orgId} />
    </div>
  );
}
