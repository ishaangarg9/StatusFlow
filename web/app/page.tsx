import Link from "next/link";
import { Activity, ShieldCheck, Bell, Globe } from "lucide-react";
import { Button } from "@/components/ui/button";

const FEATURES = [
  {
    Icon: Activity,
    title: "Scheduled HTTP checks",
    body: "A worker pings your endpoints on an interval and records every result.",
  },
  {
    Icon: Bell,
    title: "Incidents & timelines",
    body: "Open incidents manually or automatically, post updates, and resolve.",
  },
  {
    Icon: Globe,
    title: "Public status pages",
    body: "Publish a clean status page per organization — internal details stay hidden.",
  },
  {
    Icon: ShieldCheck,
    title: "Provable tenant isolation",
    body: "Postgres row-level security backstops every query, proven by a test suite.",
  },
];

export default function Home() {
  return (
    <div className="flex min-h-screen flex-col">
      <header className="flex h-14 items-center justify-between px-6">
        <span className="flex items-center gap-2 font-semibold">
          <Activity className="h-5 w-5" />
          StatusFlow
        </span>
        <nav className="flex items-center gap-2">
          <Button asChild variant="ghost" size="sm">
            <Link href="/pricing">Pricing</Link>
          </Button>
          <Button asChild variant="ghost" size="sm">
            <Link href="/login">Sign in</Link>
          </Button>
          <Button asChild size="sm">
            <Link href="/signup">Get started</Link>
          </Button>
        </nav>
      </header>

      <main className="mx-auto flex w-full max-w-4xl flex-1 flex-col px-6">
        <section className="py-20 text-center">
          <h1 className="text-balance text-4xl font-semibold tracking-tight sm:text-5xl">
            Uptime monitoring with tenant isolation you can prove.
          </h1>
          <p className="mx-auto mt-4 max-w-2xl text-balance text-muted-foreground">
            StatusFlow is a multi-tenant uptime and status-page platform. Every
            tenant&apos;s data is isolated at the database layer — not just in
            application code.
          </p>
          <div className="mt-8 flex justify-center gap-3">
            <Button asChild size="lg">
              <Link href="/signup">Create your account</Link>
            </Button>
            <Button asChild size="lg" variant="outline">
              <Link href="/status/demo">See an example status page</Link>
            </Button>
          </div>
        </section>

        <section className="grid gap-6 pb-20 sm:grid-cols-2">
          {FEATURES.map(({ Icon, title, body }) => (
            <div key={title} className="rounded-lg border p-5">
              <Icon className="h-5 w-5 text-muted-foreground" />
              <h2 className="mt-3 font-medium">{title}</h2>
              <p className="mt-1 text-sm text-muted-foreground">{body}</p>
            </div>
          ))}
        </section>
      </main>

      <footer className="border-t px-6 py-6 text-center text-xs text-muted-foreground">
        StatusFlow — a portfolio project demonstrating multi-tenant SaaS
        architecture.
      </footer>
    </div>
  );
}
