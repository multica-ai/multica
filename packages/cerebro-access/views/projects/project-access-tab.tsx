"use client";

// CEREBRO-PATCH(project-access-tab): cerebro modification of upstream file

import { useState, useMemo } from "react";
import { Lock, Check } from "lucide-react";
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
import { ActorAvatar } from "@multica/views/common/actor-avatar";

type Props = { project: Project };

/**
 * ProjectAccessTab — settings surface to flip a project between "open to
 * workspace" and "restricted", and (when restricted) manage who's in.
 *
 * Going restricted is a single moment: when the user picks "Restricted",
 * the same panel reveals an inline picker for selecting which workspace
 * members keep access. Confirm runs `addProjectMember` for each picked
 * user, then flips the access mode — so nobody experiences a brief
 * lockout window between the flip and adding the first member.
 *
 * Only workspace owners/admins see the controls; regular members see a
 * read-only summary.
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

  const adminUserIDs = new Set(
    members.filter((m) => m.role === "owner" || m.role === "admin").map((m) => m.user_id),
  );
  // Pickable = workspace members minus admins (admins always have access)
  // and minus the current user (a workspace admin in practice; would be
  // confusing to ask them to add themselves).
  const pickableMembers = useMemo(
    () => members.filter((m) => !adminUserIDs.has(m.user_id) && m.user_id !== user?.id),
    [members, adminUserIDs, user?.id],
  );

  // Pending intent — the radio choice the user has selected but not yet
  // committed. null when the visible state matches the saved state.
  const [pending, setPending] = useState<ProjectAccess | null>(null);
  const [pickedIds, setPickedIds] = useState<Set<string>>(new Set());
  const [committing, setCommitting] = useState(false);

  const next = pending ?? project.access;
  const showRestrictPicker =
    pending === "restricted" && project.access !== "restricted";
  const showOpenReview =
    pending === "workspace" && project.access === "restricted";

  const handleSelectRestricted = () => {
    setPending("restricted");
    setPickedIds(new Set());
  };

  const handleConfirmRestrict = async () => {
    setCommitting(true);
    try {
      // Add picked members FIRST so by the time access flips, the project
      // already has its members. Order matters: if the flip ran first and
      // a member-add failed, the project would be admin-only with the
      // intended members missing — exactly the lockout this UX is meant
      // to avoid.
      for (const userId of Array.from(pickedIds)) {
        try {
          await api.addProjectMember(project.id, userId);
        } catch (e) {
          toast.error(`Failed to add member: ${e instanceof Error ? e.message : String(e)}`);
          // Stop early so the access flip doesn't run with a half-applied
          // member list.
          setCommitting(false);
          return;
        }
      }
      const updated = await api.updateProjectAccess(project.id, "restricted");
      qc.invalidateQueries({ queryKey: projectKeys.list(wsId) });
      qc.invalidateQueries({ queryKey: ["projects", project.id, "members"] });
      qc.setQueryData(projectKeys.detail(wsId, project.id), updated);
      toast.success(
        pickedIds.size === 0
          ? "Project is now restricted (admins only)"
          : `Project is now restricted (${pickedIds.size} member${pickedIds.size === 1 ? "" : "s"} + admins)`,
      );
      setPending(null);
      setPickedIds(new Set());
    } finally {
      setCommitting(false);
    }
  };

  const openMutation = useMutation({
    mutationFn: () => api.updateProjectAccess(project.id, "workspace"),
    onSuccess: (updated) => {
      qc.invalidateQueries({ queryKey: projectKeys.list(wsId) });
      qc.invalidateQueries({ queryKey: ["projects", project.id, "members"] });
      qc.setQueryData(projectKeys.detail(wsId, project.id), updated);
      toast.success("Project is open to the workspace");
      setPending(null);
    },
    onError: (e) =>
      toast.error(e instanceof Error ? e.message : "Failed to change access"),
  });

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
            label="Open to workspace"
            description="Every member of this workspace can see this project, its issues, and everything inside it."
            checked={next === "workspace"}
            onSelect={() => setPending("workspace")}
          />
          <AccessOption
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
            onSelect={handleSelectRestricted}
          />
        </div>
      </div>

      {showRestrictPicker && (
        <RestrictPicker
          pickable={pickableMembers}
          pickedIds={pickedIds}
          onTogglePicked={(id) => {
            setPickedIds((prev) => {
              const out = new Set(prev);
              if (out.has(id)) out.delete(id);
              else out.add(id);
              return out;
            });
          }}
          loading={committing}
          onCancel={() => {
            setPending(null);
            setPickedIds(new Set());
          }}
          onConfirm={handleConfirmRestrict}
        />
      )}

      {showOpenReview && (
        <div
          data-testid="access-review-panel"
          className="rounded-md border border-border bg-muted/40 p-4"
        >
          <p className="text-sm">
            Open this project to the whole workspace? Everyone will be able
            to see it again.
          </p>
          <div className="mt-3 flex items-center justify-end gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={() => setPending(null)}
              disabled={openMutation.isPending}
            >
              Cancel
            </Button>
            <Button
              size="sm"
              onClick={() => openMutation.mutate()}
              disabled={openMutation.isPending}
            >
              {openMutation.isPending ? "Saving…" : "Open to workspace"}
            </Button>
          </div>
        </div>
      )}

      {project.access === "restricted" && pending === null && (
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

function RestrictPicker({
  pickable,
  pickedIds,
  onTogglePicked,
  loading,
  onCancel,
  onConfirm,
}: {
  pickable: Array<{ user_id: string; name: string; email: string }>;
  pickedIds: Set<string>;
  onTogglePicked: (userId: string) => void;
  loading: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const [search, setSearch] = useState("");
  const filtered = pickable.filter(
    (m) =>
      search === "" ||
      m.name.toLowerCase().includes(search.toLowerCase()) ||
      m.email.toLowerCase().includes(search.toLowerCase()),
  );
  return (
    <div
      data-testid="access-review-panel"
      className="rounded-md border border-border bg-muted/40 overflow-hidden"
    >
      <div className="px-4 py-3 border-b border-border text-xs">
        <strong className="text-foreground">Pick who can see this project</strong>{" "}
        <span className="text-muted-foreground">
          · admins always have access
        </span>
      </div>

      <div className="px-4 py-3 border-b border-border">
        <Input
          placeholder="Search workspace members…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          aria-label="Search workspace members"
        />
      </div>

      <ul
        className="max-h-72 overflow-auto"
        data-testid="restrict-picker-list"
      >
        {pickable.length === 0 && (
          <li className="px-4 py-3 text-xs text-muted-foreground">
            No additional members to pick — only admins will see this project.
          </li>
        )}
        {pickable.length > 0 && filtered.length === 0 && (
          <li className="px-4 py-3 text-xs text-muted-foreground">
            No matches.
          </li>
        )}
        {filtered.map((m) => {
          const picked = pickedIds.has(m.user_id);
          return (
            <li key={m.user_id} className="border-t first:border-t-0 border-border/50">
              <button
                type="button"
                role="checkbox"
                aria-checked={picked}
                onClick={() => onTogglePicked(m.user_id)}
                className={`flex w-full items-center gap-3 px-4 py-2 text-sm text-left ${
                  picked ? "bg-accent/30" : "hover:bg-accent/10"
                }`}
              >
                <span
                  className={`inline-flex size-4 shrink-0 items-center justify-center rounded border ${
                    picked
                      ? "border-foreground bg-foreground text-background"
                      : "border-border"
                  }`}
                >
                  {picked && <Check className="size-3" strokeWidth={3} />}
                </span>
                <ActorAvatar actorType="member" actorId={m.user_id} size={20} />
                <span className="flex-1 min-w-0">
                  <span className="block truncate">{m.name}</span>
                  <span className="block text-xs text-muted-foreground truncate">
                    {m.email}
                  </span>
                </span>
              </button>
            </li>
          );
        })}
      </ul>

      <div className="flex items-center justify-between gap-2 px-4 py-3 border-t border-border bg-background">
        <span className="text-xs text-muted-foreground">
          {pickedIds.size === 0
            ? "Nobody selected — only admins will have access."
            : `${pickedIds.size} ${pickedIds.size === 1 ? "person" : "people"} + admins will have access`}
        </span>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" onClick={onCancel} disabled={loading}>
            Cancel
          </Button>
          <Button size="sm" onClick={onConfirm} disabled={loading}>
            {loading ? "Saving…" : "Make restricted"}
          </Button>
        </div>
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
