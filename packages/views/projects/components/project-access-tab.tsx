"use client";

import { useState } from "react";
import { Lock } from "lucide-react";
import { useQuery, useQueryClient, useMutation } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";
import { projectKeys } from "@multica/core/projects/queries";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import type { Project, ProjectAccess, ProjectMember } from "@multica/core/types";
import { ActorAvatar } from "../../common/actor-avatar";

type Props = { project: Project };

/**
 * ProjectAccessTab — settings surface to flip a project between
 * "open to workspace" and "restricted", and (when restricted) manage
 * who's in. Only visible to workspace owners/admins; members get
 * a read-only summary.
 *
 * The signature interaction is the inline review-before-commit panel:
 * picking a different access mode reveals a panel listing who will
 * gain or lose access. The toggle doesn't apply until the user
 * confirms — no modal stack, no surprise lockouts.
 */
export function ProjectAccessTab({ project }: Props) {
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const user = useAuthStore((s) => s.user);

  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const currentMember = members.find((m) => m.user_id === user?.id);
  const isAdmin =
    currentMember?.role === "owner" || currentMember?.role === "admin";

  const projectMembersQuery = useQuery({
    queryKey: ["projects", project.id, "members"],
    queryFn: () => api.listProjectMembers(project.id),
    enabled: project.access === "restricted",
  });

  const projectMemberUserIDs = new Set(
    (projectMembersQuery.data?.members ?? []).map((m) => m.user_id),
  );
  const adminUserIDs = new Set(
    members.filter((m) => m.role === "owner" || m.role === "admin").map((m) => m.user_id),
  );

  // Pending intent — the radio choice the user has selected but not yet
  // committed. null when the visible state matches the saved state.
  const [pending, setPending] = useState<ProjectAccess | null>(null);

  const updateAccess = useMutation({
    mutationFn: (access: ProjectAccess) =>
      api.updateProjectAccess(project.id, access),
    onSuccess: (updated) => {
      qc.invalidateQueries({ queryKey: projectKeys.list(wsId) });
      qc.invalidateQueries({ queryKey: ["projects", project.id, "members"] });
      qc.setQueryData(projectKeys.detail(wsId, project.id), updated);
      toast.success(
        updated.access === "restricted"
          ? "Project is now restricted"
          : "Project is open to the workspace",
      );
      setPending(null);
    },
    onError: (e) =>
      toast.error(e instanceof Error ? e.message : "Failed to change access"),
  });

  const next = pending ?? project.access;

  // Members about to be affected: people who will lose access when going
  // restricted = workspace members who are NOT admins and NOT in the
  // project_member set.
  const losingAccess = members.filter(
    (m) =>
      !adminUserIDs.has(m.user_id) &&
      !projectMemberUserIDs.has(m.user_id) &&
      m.user_id !== user?.id,
  );
  // Going from restricted to open: everyone outside admins + project_members
  // gains access.
  const gainingAccess = losingAccess;

  const showReview = pending !== null && pending !== project.access;

  if (!isAdmin) {
    return (
      <div className="p-6 text-sm text-muted-foreground">
        <div className="mb-1 font-medium text-foreground">
          {project.access === "restricted"
            ? "This project is restricted"
            : "Open to the workspace"}
        </div>
        <p>
          {project.access === "restricted"
            ? "Only selected members and workspace admins can see this project. Ask an admin to change access."
            : "Every member of this workspace can see this project."}
        </p>
      </div>
    );
  }

  return (
    <div className="p-6 space-y-6 max-w-2xl" data-testid="project-access-tab">
      <div>
        <h3 className="text-sm font-semibold mb-1">Who can see this project</h3>
        <p className="text-xs text-muted-foreground mb-4">
          Workspace owners and admins always have access.
        </p>

        <div className="rounded-md border border-border divide-y">
          <AccessOption
            value="workspace"
            label="Open to workspace"
            description={`Every member of this workspace can see this project, its issues, and everything inside it.`}
            checked={next === "workspace"}
            onSelect={() => setPending("workspace")}
          />
          <AccessOption
            value="restricted"
            label={
              <span className="inline-flex items-center gap-2">
                Restricted
                <span className="inline-flex items-center gap-1 rounded-full border border-border bg-background px-2 py-0.5 text-[11px] text-muted-foreground">
                  <Lock className="size-3" /> selected people only
                </span>
              </span>
            }
            description="Pick who can see this project. Workspace admins keep access automatically. AI agents triggered on issues here cannot see anything outside this project."
            checked={next === "restricted"}
            onSelect={() => setPending("restricted")}
          />
        </div>
      </div>

      {showReview && pending === "restricted" && (
        <ReviewPanel
          heading={`${losingAccess.length} ${
            losingAccess.length === 1 ? "person" : "people"
          } will lose access to this project`}
          members={losingAccess}
          confirmLabel="Make restricted"
          loading={updateAccess.isPending}
          onCancel={() => setPending(null)}
          onConfirm={() => updateAccess.mutate("restricted")}
        />
      )}

      {showReview && pending === "workspace" && (
        <ReviewPanel
          heading={`${gainingAccess.length} ${
            gainingAccess.length === 1 ? "person" : "people"
          } will gain access to this project`}
          members={gainingAccess}
          confirmLabel="Open to workspace"
          loading={updateAccess.isPending}
          onCancel={() => setPending(null)}
          onConfirm={() => updateAccess.mutate("workspace")}
        />
      )}

      {project.access === "restricted" && !showReview && (
        <ProjectMembersPanel
          project={project}
          projectMembers={projectMembersQuery.data?.members ?? []}
          adminUserIDs={adminUserIDs}
          allMembers={members.map((m) => ({
            user_id: m.user_id,
            name: m.name,
            email: m.email,
            avatar_url: m.avatar_url,
            role: m.role,
          }))}
          onMutated={() =>
            qc.invalidateQueries({ queryKey: ["projects", project.id, "members"] })
          }
        />
      )}
    </div>
  );
}

function AccessOption({
  label,
  description,
  checked,
  onSelect,
}: {
  value: string;
  label: React.ReactNode;
  description: string;
  checked: boolean;
  onSelect: () => void;
}) {
  return (
    <button
      type="button"
      role="radio"
      aria-checked={checked}
      onClick={onSelect}
      className={`flex w-full items-start gap-4 p-4 text-left ${
        checked ? "bg-accent/30" : "hover:bg-accent/10"
      }`}
    >
      <span
        className={`mt-0.5 inline-flex size-4 shrink-0 items-center justify-center rounded-full border ${
          checked ? "border-foreground" : "border-border"
        }`}
      >
        {checked && <span className="size-2 rounded-full bg-foreground" />}
      </span>
      <span className="flex-1 min-w-0">
        <span className="block text-sm font-medium">{label}</span>
        <span className="block mt-1 text-xs text-muted-foreground max-w-prose">
          {description}
        </span>
      </span>
    </button>
  );
}

function ReviewPanel({
  heading,
  members,
  confirmLabel,
  loading,
  onCancel,
  onConfirm,
}: {
  heading: string;
  members: Array<{ user_id: string; name: string; email: string }>;
  confirmLabel: string;
  loading: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const visible = members.slice(0, 4);
  const more = Math.max(0, members.length - visible.length);
  return (
    <div
      data-testid="access-review-panel"
      className="rounded-md border border-border bg-muted/40 overflow-hidden"
    >
      <div className="px-4 py-3 border-b border-border text-xs">
        <strong className="text-foreground">Review change</strong>{" "}
        <span className="text-muted-foreground">· {heading}</span>
      </div>
      <ul className="px-4 py-2 text-sm">
        {visible.map((m) => (
          <li
            key={m.user_id}
            className="flex items-center gap-3 py-1.5 border-t first:border-t-0 border-border/50"
          >
            <ActorAvatar actorType="member" actorId={m.user_id} size={20} />
            <span className="flex-1 min-w-0 truncate">{m.name}</span>
            <span className="text-[11px] text-muted-foreground">member</span>
          </li>
        ))}
        {more > 0 && (
          <li className="py-1.5 text-xs text-muted-foreground">
            + {more} more
          </li>
        )}
        {visible.length === 0 && (
          <li className="py-1.5 text-xs text-muted-foreground">
            Nobody is affected.
          </li>
        )}
      </ul>
      <div className="flex items-center justify-end gap-2 px-4 py-3 border-t border-border bg-background">
        <Button variant="outline" size="sm" onClick={onCancel} disabled={loading}>
          Cancel
        </Button>
        <Button size="sm" onClick={onConfirm} disabled={loading}>
          {loading ? "Saving…" : confirmLabel}
        </Button>
      </div>
    </div>
  );
}

// ---- Project members panel (renders inside the access tab) ----

function ProjectMembersPanel({
  project,
  projectMembers,
  adminUserIDs,
  allMembers,
  onMutated,
}: {
  project: Project;
  projectMembers: ProjectMember[];
  adminUserIDs: Set<string>;
  allMembers: Array<{
    user_id: string;
    name: string;
    email: string;
    avatar_url: string | null;
    role: string;
  }>;
  onMutated: () => void;
}) {
  const [search, setSearch] = useState("");
  const [busy, setBusy] = useState<string | null>(null);

  const projectMemberIDs = new Set(projectMembers.map((m) => m.user_id));

  const candidates = allMembers.filter(
    (m) =>
      !projectMemberIDs.has(m.user_id) &&
      !adminUserIDs.has(m.user_id) &&
      (search === "" ||
        m.name.toLowerCase().includes(search.toLowerCase()) ||
        m.email.toLowerCase().includes(search.toLowerCase())),
  );

  const handleAdd = async (userId: string) => {
    setBusy(userId);
    try {
      await api.addProjectMember(project.id, userId);
      toast.success("Member added");
      onMutated();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to add");
    } finally {
      setBusy(null);
    }
  };
  const handleRemove = async (userId: string) => {
    setBusy(userId);
    try {
      await api.removeProjectMember(project.id, userId);
      toast.success("Member removed");
      onMutated();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to remove");
    } finally {
      setBusy(null);
    }
  };

  const adminMembers = allMembers.filter((m) => adminUserIDs.has(m.user_id));

  return (
    <div className="space-y-4" data-testid="project-members-panel">
      <div>
        <h4 className="text-sm font-semibold mb-2">Project members</h4>
        <ul className="rounded-md border border-border divide-y">
          {projectMembers.length === 0 && (
            <li className="px-4 py-3 text-xs text-muted-foreground">
              No project members yet. Add someone below.
            </li>
          )}
          {projectMembers.map((m) => (
            <li
              key={m.user_id}
              className="flex items-center gap-3 px-4 py-2 text-sm"
            >
              <ActorAvatar actorType="member" actorId={m.user_id} size={24} />
              <span className="flex-1 min-w-0">
                <span className="block truncate">{m.name}</span>
                <span className="block text-xs text-muted-foreground truncate">
                  {m.email}
                </span>
              </span>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => handleRemove(m.user_id)}
                disabled={busy === m.user_id}
              >
                Remove
              </Button>
            </li>
          ))}
        </ul>
      </div>

      <div>
        <h4 className="text-xs uppercase tracking-wide text-muted-foreground mb-2">
          Always-on access · workspace governance
        </h4>
        <ul className="rounded-md border border-border divide-y">
          {adminMembers.map((m) => (
            <li
              key={m.user_id}
              className="flex items-center gap-3 px-4 py-2 text-sm"
              data-testid="always-on-row"
            >
              <ActorAvatar actorType="member" actorId={m.user_id} size={24} />
              <span className="flex-1 min-w-0">
                <span className="block truncate">{m.name}</span>
                <span className="block text-xs text-muted-foreground truncate">
                  {m.email} · workspace {m.role}
                </span>
              </span>
              <span className="text-[11px] text-muted-foreground">
                always-on
              </span>
            </li>
          ))}
        </ul>
      </div>

      <div>
        <h4 className="text-sm font-semibold mb-2">Add member</h4>
        <Input
          placeholder="Search workspace members…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          aria-label="Search workspace members"
        />
        {search && (
          <ul className="mt-2 rounded-md border border-border divide-y max-h-60 overflow-auto">
            {candidates.length === 0 && (
              <li className="px-4 py-2 text-xs text-muted-foreground">
                No matches.
              </li>
            )}
            {candidates.map((m) => (
              <li
                key={m.user_id}
                className="flex items-center gap-3 px-4 py-2 text-sm"
              >
                <ActorAvatar actorType="member" actorId={m.user_id} size={20} />
                <span className="flex-1 min-w-0 truncate">{m.name}</span>
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => handleAdd(m.user_id)}
                  disabled={busy === m.user_id}
                >
                  Add
                </Button>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}
