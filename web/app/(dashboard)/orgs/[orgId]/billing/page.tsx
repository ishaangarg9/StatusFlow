import { Suspense } from "react";
import { BillingPanel } from "@/components/billing-panel";

// Org-scoped billing. Membership is guaranteed by the [orgId] layout; the panel
// itself gates on billing:read/manage (convenience) while the API enforces. The
// panel reads ?checkout= via useSearchParams, so it sits behind a Suspense
// boundary per the App Router contract.
export default async function BillingPage({
  params,
}: {
  params: Promise<{ orgId: string }>;
}) {
  const { orgId } = await params;
  return (
    <div className="mx-auto w-full max-w-2xl space-y-6 px-6 py-10">
      <h1 className="text-2xl font-semibold">Billing</h1>
      <Suspense>
        <BillingPanel orgId={orgId} />
      </Suspense>
    </div>
  );
}
