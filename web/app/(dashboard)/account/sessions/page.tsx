import { SessionsManager } from "@/components/sessions-manager";

export default function SessionsPage() {
  return (
    <main className="mx-auto w-full max-w-2xl px-6 py-10">
      <h1 className="mb-6 text-2xl font-semibold">Sessions</h1>
      <SessionsManager />
    </main>
  );
}
