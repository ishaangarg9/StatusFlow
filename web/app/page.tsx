import Link from "next/link";

export default function Home() {
  return (
    <main className="mx-auto max-w-2xl px-6 py-24">
      <h1 className="text-3xl font-semibold">StatusFlow</h1>
      <p className="mt-2 text-neutral-600 dark:text-neutral-400">
        Multi-tenant uptime monitoring. Sign in to your org, or visit a public
        status page.
      </p>
      <div className="mt-8 flex gap-4">
        <Link
          href="/login"
          className="rounded-md bg-neutral-900 px-4 py-2 text-white dark:bg-white dark:text-neutral-900"
        >
          Sign in
        </Link>
        <Link
          href="/status/demo"
          className="rounded-md border border-neutral-300 px-4 py-2 dark:border-neutral-700"
        >
          See an example status page
        </Link>
      </div>
    </main>
  );
}
