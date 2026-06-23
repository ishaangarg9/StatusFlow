"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Trash2 } from "lucide-react";
import { api } from "@/lib/api/client";
import { errorMessage } from "@/lib/errors";
import { qk } from "@/lib/query-keys";
import { formatDate } from "@/lib/utils";
import type { Role } from "@/lib/types";
import { useCan } from "@/components/can";
import { ConfirmDialog } from "@/components/confirm-dialog";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

const INVITE_ROLES: Role[] = ["admin", "member", "viewer"];
const ALL_ROLES: Role[] = ["owner", "admin", "member", "viewer"];

export function MembersManager({
  orgId,
  currentUserId,
}: {
  orgId: string;
  currentUserId: string;
}) {
  const router = useRouter();
  const qc = useQueryClient();
  const canManage = useCan("member:role:update");
  const canRemove = useCan("member:remove");
  const canInvite = useCan("member:invite");

  const { data: members, isLoading } = useQuery({
    queryKey: qk.members(orgId),
    queryFn: () => api.listMembers(orgId),
  });

  const changeRole = useMutation({
    mutationFn: ({ userId, role }: { userId: string; role: Role }) =>
      api.updateMemberRole(orgId, userId, role),
    onSuccess: () => {
      toast.success("Role updated");
      qc.invalidateQueries({ queryKey: qk.members(orgId) });
      qc.invalidateQueries({ queryKey: qk.me });
      // The caller's own role (e.g. after transferring ownership) is provided
      // by the server [orgId] layout via RoleProvider, not a client query, so
      // refresh the server tree to re-render role-gated controls accurately.
      router.refresh();
    },
    onError: (e) => toast.error(errorMessage(e)),
  });

  const removeMember = useMutation({
    mutationFn: (userId: string) => api.removeMember(orgId, userId),
    onSuccess: () => {
      toast.success("Member removed");
      qc.invalidateQueries({ queryKey: qk.members(orgId) });
    },
    onError: (e) => toast.error(errorMessage(e)),
  });

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader>
          <CardTitle>Members</CardTitle>
          <CardDescription>
            People with access to this organization.
          </CardDescription>
        </CardHeader>
        <CardContent>
          {isLoading ? (
            <Skeleton className="h-32 w-full" />
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Member</TableHead>
                  <TableHead>Role</TableHead>
                  <TableHead>Joined</TableHead>
                  <TableHead className="w-10" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {members?.map((m) => {
                  const isOwner = m.role === "owner";
                  const isSelf = m.userId === currentUserId;
                  return (
                    <TableRow key={m.userId}>
                      <TableCell>
                        <div className="font-medium">{m.name || m.email}</div>
                        {m.name && (
                          <div className="text-xs text-muted-foreground">
                            {m.email}
                          </div>
                        )}
                      </TableCell>
                      <TableCell>
                        {canManage && !isOwner ? (
                          <RoleSelect
                            value={m.role}
                            onChange={(role) =>
                              changeRole.mutate({ userId: m.userId, role })
                            }
                            disabled={changeRole.isPending}
                          />
                        ) : (
                          <Badge variant="secondary">{m.role}</Badge>
                        )}
                      </TableCell>
                      <TableCell className="text-sm text-muted-foreground">
                        {formatDate(m.createdAt)}
                      </TableCell>
                      <TableCell>
                        {canRemove && !isOwner && !isSelf && (
                          <ConfirmDialog
                            title="Remove member?"
                            description={`${m.email} will lose access to this organization.`}
                            confirmLabel="Remove"
                            onConfirm={() => removeMember.mutateAsync(m.userId)}
                            trigger={
                              <Button variant="ghost" size="icon">
                                <Trash2 className="h-4 w-4 text-destructive" />
                              </Button>
                            }
                          />
                        )}
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      {canInvite && <InvitationsPanel orgId={orgId} />}
    </div>
  );
}

function RoleSelect({
  value,
  onChange,
  disabled,
}: {
  value: Role;
  onChange: (role: Role) => void;
  disabled?: boolean;
}) {
  // Promoting to owner transfers ownership (the current owner is demoted to
  // admin server-side); confirm that explicitly with a controlled dialog.
  const [pendingOwner, setPendingOwner] = useState(false);

  return (
    <>
      <Select
        value={value}
        onValueChange={(v) => {
          const role = v as Role;
          if (role === "owner") setPendingOwner(true);
          else onChange(role);
        }}
        disabled={disabled}
      >
        <SelectTrigger className="h-8 w-28">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {ALL_ROLES.map((r) => (
            <SelectItem key={r} value={r}>
              {r}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <AlertDialog open={pendingOwner} onOpenChange={setPendingOwner}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Transfer ownership?</AlertDialogTitle>
            <AlertDialogDescription>
              This person becomes the owner and you are demoted to admin.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                onChange("owner");
                setPendingOwner(false);
              }}
            >
              Transfer ownership
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

function InvitationsPanel({ orgId }: { orgId: string }) {
  const qc = useQueryClient();
  const [email, setEmail] = useState("");
  const [role, setRole] = useState<Role>("member");

  const { data: invitations, isLoading } = useQuery({
    queryKey: qk.invitations(orgId),
    queryFn: () => api.listInvitations(orgId),
  });

  const invite = useMutation({
    mutationFn: () => api.createInvitation(orgId, { email, role }),
    onSuccess: () => {
      toast.success("Invitation sent");
      setEmail("");
      qc.invalidateQueries({ queryKey: qk.invitations(orgId) });
    },
    onError: (e) => toast.error(errorMessage(e)),
  });

  const revoke = useMutation({
    mutationFn: (id: string) => api.revokeInvitation(orgId, id),
    onSuccess: () => {
      toast.success("Invitation revoked");
      qc.invalidateQueries({ queryKey: qk.invitations(orgId) });
    },
    onError: (e) => toast.error(errorMessage(e)),
  });

  return (
    <Card>
      <CardHeader>
        <CardTitle>Invitations</CardTitle>
        <CardDescription>
          Invite teammates by email. Pending invites expire after 7 days.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <form
          className="flex flex-wrap items-end gap-3"
          onSubmit={(e) => {
            e.preventDefault();
            invite.mutate();
          }}
        >
          <div className="flex-1 space-y-1.5">
            <Label htmlFor="invite-email">Email</Label>
            <Input
              id="invite-email"
              type="email"
              required
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              placeholder="teammate@example.com"
            />
          </div>
          <div className="space-y-1.5">
            <Label>Role</Label>
            <Select value={role} onValueChange={(v) => setRole(v as Role)}>
              <SelectTrigger className="w-28">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {INVITE_ROLES.map((r) => (
                  <SelectItem key={r} value={r}>
                    {r}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <Button type="submit" disabled={invite.isPending || !email}>
            Invite
          </Button>
        </form>

        {isLoading ? (
          <Skeleton className="h-12 w-full" />
        ) : invitations && invitations.length > 0 ? (
          <ul className="divide-y rounded-md border">
            {invitations.map((inv) => (
              <li
                key={inv.id}
                className="flex items-center justify-between p-3"
              >
                <div>
                  <span className="text-sm font-medium">{inv.email}</span>
                  <Badge variant="outline" className="ml-2">
                    {inv.role}
                  </Badge>
                </div>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => revoke.mutate(inv.id)}
                  disabled={revoke.isPending}
                >
                  Revoke
                </Button>
              </li>
            ))}
          </ul>
        ) : (
          <p className="text-sm text-muted-foreground">No pending invitations.</p>
        )}
      </CardContent>
    </Card>
  );
}
