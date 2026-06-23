import { LoginForm } from "@/components/forms/login-form";
import { safeNext } from "@/lib/nav";

export default async function LoginPage({
  searchParams,
}: {
  searchParams: Promise<{ next?: string }>;
}) {
  const { next } = await searchParams;
  // safeNext rejects protocol-relative/backslash forms (open-redirect vectors);
  // fall back to /orgs when next is absent or unsafe.
  return <LoginForm next={safeNext(next) ?? "/orgs"} />;
}
