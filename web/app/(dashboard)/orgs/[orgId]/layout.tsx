import { headers } from "next/headers";
import { notFound, redirect } from "next/navigation";
import { getMe } from "@/lib/auth";
import { loginPath } from "@/lib/nav";
import { RoleProvider } from "@/components/role-context";
import { OrgNav } from "@/components/org-nav";

// Org-scoped guard. Mirrors the API's 404-hides-existence contract: a user with
// no membership in this org gets notFound(), never a hint the org exists. The
// caller's role is provided to client components for convenience gating.
export default async function OrgLayout({
  children,
  params,
}: {
  children: React.ReactNode;
  params: Promise<{ orgId: string }>;
}) {
  const { orgId } = await params;
  const me = await getMe();
  if (!me) redirect(loginPath((await headers()).get("x-pathname")));

  const membership = me.memberships.find((m) => m.orgId === orgId);
  if (!membership) notFound();

  return (
    <RoleProvider role={membership.role}>
      <OrgNav orgId={orgId} />
      <main className="min-w-0 flex-1">{children}</main>
    </RoleProvider>
  );
}
