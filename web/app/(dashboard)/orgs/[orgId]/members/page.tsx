import { redirect } from "next/navigation";
import { getMe } from "@/lib/auth";
import { MembersManager } from "@/components/members-manager";

export default async function MembersPage({
  params,
}: {
  params: Promise<{ orgId: string }>;
}) {
  const { orgId } = await params;
  const me = await getMe();
  if (!me) redirect("/login");

  return (
    <div className="mx-auto w-full max-w-4xl px-6 py-8">
      <h1 className="mb-6 text-2xl font-semibold">Team</h1>
      <MembersManager orgId={orgId} currentUserId={me.user.id} />
    </div>
  );
}
