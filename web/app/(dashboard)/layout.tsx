import { headers } from "next/headers";
import { redirect } from "next/navigation";
import { getMe } from "@/lib/auth";
import { loginPath } from "@/lib/nav";
import { AppShell } from "@/components/app-shell";

// Server guard for every authenticated screen. Validates the session (real
// /me call, unlike the cheap middleware presence check) and renders the shell.
export default async function DashboardLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const me = await getMe();
  if (!me) {
    // Cookie was present (passed middleware) but invalid/expired. Preserve the
    // deep link via the path the middleware stamped on the request.
    const path = (await headers()).get("x-pathname");
    redirect(loginPath(path));
  }
  return <AppShell me={me}>{children}</AppShell>;
}
