import { AcceptInvitation } from "@/components/accept-invitation";

// Authenticated (the dashboard layout guards it). If the invitee isn't signed
// in, getMe -> redirect to /login?next=/invitations/accept?token=…, then back.
export default async function AcceptInvitationPage({
  searchParams,
}: {
  searchParams: Promise<{ token?: string }>;
}) {
  const { token } = await searchParams;
  return (
    <div className="mx-auto w-full max-w-md px-6 py-10">
      <AcceptInvitation token={token ?? null} />
    </div>
  );
}
