import { serverApi } from "@/lib/api/server";
import { OrgSettings } from "@/components/org-settings";

export default async function SettingsPage({
  params,
}: {
  params: Promise<{ orgId: string }>;
}) {
  const { orgId } = await params;
  const org = await serverApi.getOrg(orgId);

  return (
    <div className="mx-auto w-full max-w-2xl px-6 py-8">
      <h1 className="mb-6 text-2xl font-semibold">Settings</h1>
      <OrgSettings org={org} />
    </div>
  );
}
