"use client";

import { useState, useMemo, useEffect } from "react";
import { Workflow, Save, UserPlus } from "lucide-react";
import { ActorAvatar } from "../../common/actor-avatar";
import { Button } from "@multica/ui/components/ui/button";
import { Switch } from "@multica/ui/components/ui/switch";
import { Input } from "@multica/ui/components/ui/input";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";
import { useWorkflowAdmins, useUpdateWorkflowAdmins } from "@multica/core/workflows/queries";
import { api } from "@multica/core/api";
import { useT } from "../../i18n";

function arraysEqual(a: string[], b: string[]): boolean {
  if (a.length !== b.length) return false;
  const sortedA = [...a].sort();
  const sortedB = [...b].sort();
  return sortedA.every((v, i) => v === sortedB[i]);
}

function AdminRow({
  name,
  email,
  userId,
  isChecked,
  busy,
  onToggle,
}: {
  name: string;
  email: string;
  userId: string;
  isChecked: boolean;
  busy: boolean;
  onToggle: (checked: boolean) => void;
}) {
  return (
    <div className="flex items-center gap-3 px-4 py-3">
      <ActorAvatar actorType="member" actorId={userId} size={32} />
      <div className="min-w-0 flex-1">
        <div className="text-sm font-medium truncate">{name}</div>
        <div className="text-xs text-muted-foreground truncate">{email}</div>
      </div>
      <Switch
        size="sm"
        checked={isChecked}
        onCheckedChange={onToggle}
        disabled={busy}
      />
    </div>
  );
}

function LoadingSkeleton() {
  return (
    <section className="space-y-4">
      <div className="flex items-center gap-2">
        <Skeleton className="h-4 w-4" />
        <Skeleton className="h-5 w-32" />
      </div>
      <div className="overflow-hidden rounded-xl ring-1 ring-foreground/10">
        {Array.from({ length: 3 }).map((_, i) => (
          <div key={i} className={i > 0 ? "border-t border-border/50" : ""}>
            <div className="flex items-center gap-3 px-4 py-3">
              <Skeleton className="size-8 rounded-full" />
              <div className="min-w-0 flex-1 space-y-1">
                <Skeleton className="h-4 w-32" />
                <Skeleton className="h-3 w-48" />
              </div>
              <Skeleton className="h-[14px] w-6 rounded-full" />
            </div>
          </div>
        ))}
      </div>
    </section>
  );
}

export function WorkflowAdminsTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const user = useAuthStore((s) => s.user);

  const { data: members = [], isLoading: membersLoading } = useQuery(memberListOptions(wsId));
  const { data: admins = [], isLoading: adminsLoading } = useWorkflowAdmins();

  const updateMutation = useUpdateWorkflowAdmins();

  // Check if current user is a workflow admin (by user.id matching admin.id)
  const isWorkflowAdmin = user ? admins.some((a) => a.id === user.id) : false;

  const [selectedIds, setSelectedIds] = useState<string[]>([]);
  const [initialized, setInitialized] = useState(false);
  const [inviteEmail, setInviteEmail] = useState("");
  const [inviting, setInviting] = useState(false);

  // Sync admin list into local selection state on initial load
  useEffect(() => {
    if (!adminsLoading && !initialized) {
      setSelectedIds(admins.map((a) => a.id));
      setInitialized(true);
    }
  }, [adminsLoading, initialized]);

  const hasChanges = useMemo(() => {
    if (!initialized) return false;
    return !arraysEqual(selectedIds, admins.map((a) => a.id));
  }, [selectedIds, admins, initialized]);

  const handleToggle = (userId: string, checked: boolean) => {
    setSelectedIds((prev) =>
      checked ? [...prev, userId] : prev.filter((id) => id !== userId),
    );
  };

  const handleSave = async () => {
    try {
      await updateMutation.mutateAsync(selectedIds);
      toast.success(t(($) => $.workflow_admins.toast_saved));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.workflow_admins.toast_save_failed));
    }
  };

  const handleInvite = async () => {
    const email = inviteEmail.trim();
    if (!email) return;
    setInviting(true);
    try {
      await api.inviteWorkflowAdmin(email);
      setInviteEmail("");
      toast.success(`已邀请 ${email}`);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "邀请失败");
    } finally {
      setInviting(false);
    }
  };

  // Not a workflow admin — show permission message
  if (!isWorkflowAdmin && !adminsLoading) {
    return (
      <div className="space-y-8">
        <section className="space-y-4">
          <div className="flex items-center gap-2">
            <Workflow className="h-4 w-4 text-muted-foreground" />
            <h2 className="text-sm font-semibold">{t(($) => $.workflow_admins.section_title, { count: 0 })}</h2>
          </div>
          <p className="text-sm text-muted-foreground">{t(($) => $.workflow_admins.permission_denied)}</p>
        </section>
      </div>
    );
  }

  // Loading state
  if (membersLoading || adminsLoading) {
    return (
      <div className="space-y-8">
        <LoadingSkeleton />
      </div>
    );
  }

  // Empty state — no members
  if (members.length === 0) {
    return (
      <div className="space-y-8">
        <section className="space-y-4">
          <div className="flex items-center gap-2">
            <Workflow className="h-4 w-4 text-muted-foreground" />
            <h2 className="text-sm font-semibold">{t(($) => $.workflow_admins.section_title, { count: 0 })}</h2>
          </div>
          <p className="text-sm text-muted-foreground">{t(($) => $.workflow_admins.empty_hint)}</p>
        </section>
      </div>
    );
  }

  return (
    <div className="space-y-8">
      <section className="space-y-4">
        <div className="flex items-center gap-2">
          <Workflow className="h-4 w-4 text-muted-foreground" />
          <h2 className="text-sm font-semibold">
            {t(($) => $.workflow_admins.section_title, { count: selectedIds.length })}
          </h2>
        </div>

        <div className="flex gap-2">
          <Input
            placeholder="输入用户邮箱邀请管理员"
            value={inviteEmail}
            onChange={(e) => setInviteEmail(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && handleInvite()}
            className="max-w-sm"
          />
          <Button size="sm" onClick={handleInvite} disabled={inviting || !inviteEmail.trim()}>
            <UserPlus className="h-3.5 w-3.5 mr-1" />
            {inviting ? "邀请中..." : "邀请"}
          </Button>
        </div>

        <div className="overflow-hidden rounded-xl ring-1 ring-foreground/10">
          {members.map((member, i) => (
            <div key={member.user_id} className={i > 0 ? "border-t border-border/50" : ""}>
              <AdminRow
                name={member.name}
                email={member.email}
                userId={member.user_id}
                isChecked={selectedIds.includes(member.user_id)}
                busy={updateMutation.isPending}
                onToggle={(checked) => handleToggle(member.user_id, checked)}
              />
            </div>
          ))}
        </div>
      </section>

      {hasChanges && (
        <div className="flex justify-end">
          <Button size="sm" onClick={handleSave} disabled={updateMutation.isPending}>
            {updateMutation.isPending ? (
              <>{t(($) => $.workflow_admins.saving)}</>
            ) : (
              <>
                <Save className="h-3.5 w-3.5 mr-1" />
                {t(($) => $.workflow_admins.save)}
              </>
            )}
          </Button>
        </div>
      )}
    </div>
  );
}
