import { redirect } from "next/navigation";
import { getMe } from "@/lib/auth";
import { AppShell } from "@/components/app-shell";

// Server guard for every authenticated screen. Validates the session (real
// /me call, unlike the cheap middleware presence check) and renders the shell.
export default async function DashboardLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const me = await getMe();
  if (!me) redirect("/login");
  return <AppShell me={me}>{children}</AppShell>;
}
