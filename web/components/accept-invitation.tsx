"use client";

import { useRouter } from "next/navigation";
import { useMutation } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@/lib/api/client";
import { errorMessage } from "@/lib/errors";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

export function AcceptInvitation({ token }: { token: string | null }) {
  const router = useRouter();

  const accept = useMutation({
    mutationFn: () => {
      if (!token) throw new Error("Missing invitation token.");
      return api.acceptInvitation(token);
    },
    onSuccess: (membership) => {
      toast.success("Invitation accepted");
      router.push(`/orgs/${membership.orgId}`);
      router.refresh();
    },
    onError: (e) => toast.error(errorMessage(e)),
  });

  return (
    <Card>
      <CardHeader>
        <CardTitle>Accept invitation</CardTitle>
        <CardDescription>
          {token
            ? "You've been invited to join an organization on StatusFlow."
            : "This invitation link is missing its token."}
        </CardDescription>
      </CardHeader>
      <CardContent className="text-sm text-muted-foreground">
        Accepting will add your account to the organization with the role from
        your invitation.
      </CardContent>
      <CardFooter className="mt-2">
        <Button
          onClick={() => accept.mutate()}
          disabled={!token || accept.isPending}
        >
          {accept.isPending ? "Accepting…" : "Accept invitation"}
        </Button>
      </CardFooter>
    </Card>
  );
}
