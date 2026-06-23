import Link from "next/link";
import { Activity } from "lucide-react";

// Centered, sidebar-free shell for unauthenticated auth screens.
export default function AuthLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <div className="flex min-h-screen flex-col items-center justify-center bg-muted/30 px-4 py-12">
      <Link
        href="/"
        className="mb-8 flex items-center gap-2 text-lg font-semibold"
      >
        <Activity className="h-5 w-5" />
        StatusFlow
      </Link>
      <div className="w-full max-w-sm">{children}</div>
    </div>
  );
}
