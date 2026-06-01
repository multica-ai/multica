"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import {
  GitCompare,
  GitFork,
  GitMerge,
  GitPullRequest,
  History,
  Loader2,
  Plus,
  ShieldCheck,
  Terminal,
  X,
} from "lucide-react";
import { toast } from "sonner";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { memberListOptions, workspaceKeys } from "@multica/core/workspace/queries";
import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
import type {
  Skill,
  SkillChangeRequest,
  SkillFork,
  SkillVersion,
} from "@multica/core/types";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { AppLink, useNavigation } from "@multica/views/navigation";
import { useTimeAgo } from "@multica/views/i18n";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import {
  skillChangeRequestsOptions,
  skillForkParentOptions,
  skillForksOptions,
  skillOwnershipKeys,
  skillVersionsOptions,
} from "../../core/queries";
import { useCanManageSkill } from "../../core/use-can-manage-skill";
import { MemberPicker } from "./member-picker";
import { CreateChangeRequestDialog } from "./create-change-request-dialog";
import { LineDiff } from "./line-diff";

const SectionHeading = ({ children }: { children: React.ReactNode }) => (
  <h3 className="mb-2 flex items-center gap-1.5 text-xs font-medium uppercase tracking-wider text-muted-foreground">
    {children}
  </h3>
);

// ---------------------------------------------------------------------------
// Member name + avatar chip
// ---------------------------------------------------------------------------

function MemberChip({
  userId,
  wsId,
  fallback = "Ukendt",
}: {
  userId: string | null | undefined;
  wsId: string;
  fallback?: string;
}) {
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  if (!userId) {
    return <span className="text-muted-foreground">{fallback}</span>;
  }
  const m = members.find((x) => x.user_id === userId);
  if (!m) {
    return <span className="font-mono text-muted-foreground">{userId.slice(0, 8)}…</span>;
  }
  return (
    <span className="inline-flex items-center gap-1.5">
      <ActorAvatar
        name={m.name}
        initials={m.name.slice(0, 2).toUpperCase()}
        avatarUrl={resolvePublicFileUrl(m.avatar_url)}
        size={16}
      />
      <span className="truncate">{m.name}</span>
    </span>
  );
}

// ---------------------------------------------------------------------------
// Status badge for change requests
// ---------------------------------------------------------------------------

function statusBadge(status: SkillChangeRequest["status"]) {
  switch (status) {
    case "pending":
      return <Badge variant="outline">Afventer</Badge>;
    case "merged":
      return <Badge variant="default">Flettet</Badge>;
    case "approved":
      return <Badge variant="secondary">Godkendt</Badge>;
    case "rejected":
      return <Badge variant="destructive">Afvist</Badge>;
    default:
      return <Badge variant="ghost">{status}</Badge>;
  }
}

// ---------------------------------------------------------------------------
// Ownership section
// ---------------------------------------------------------------------------

function OwnershipSection({
  skill,
  wsId,
  canManage,
}: {
  skill: Skill;
  wsId: string;
  canManage: boolean;
}) {
  const qc = useQueryClient();

  const ownerMutation = useMutation({
    mutationFn: (ownerId: string) =>
      api.updateSkillOwnership(skill.id, { owner_id: ownerId }),
    onSuccess: () => {
      // Invalidate (not setQueryData): updateSkillOwnership returns the
      // files-less SkillResponse, but the detail query expects the full
      // SkillWithFilesResponse — overwriting it would blank the file tree.
      qc.invalidateQueries({ queryKey: [...workspaceKeys.skills(wsId), skill.id] });
      qc.invalidateQueries({ queryKey: workspaceKeys.skills(wsId), exact: true });
      toast.success("Ejer opdateret");
    },
    onError: (err) =>
      toast.error(err instanceof Error ? err.message : "Kunne ikke skifte ejer"),
  });

  const approverMutation = useMutation({
    mutationFn: (approverIds: string[]) =>
      api.updateSkillOwnership(skill.id, { approver_ids: approverIds }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: [...workspaceKeys.skills(wsId), skill.id] });
      qc.invalidateQueries({ queryKey: workspaceKeys.skills(wsId), exact: true });
    },
    onError: (err) =>
      toast.error(
        err instanceof Error ? err.message : "Kunne ikke opdatere godkendere",
      ),
  });

  const toggleApprover = (userId: string) => {
    const next = skill.approver_ids.includes(userId)
      ? skill.approver_ids.filter((id) => id !== userId)
      : [...skill.approver_ids, userId];
    approverMutation.mutate(next);
  };

  const busy = ownerMutation.isPending || approverMutation.isPending;

  return (
    <div>
      <SectionHeading>
        <ShieldCheck className="h-3 w-3" />
        Ejerskab
      </SectionHeading>
      <div className="space-y-3 text-xs">
        <div className="flex items-center gap-2">
          <span className="min-w-16 text-muted-foreground">Ejer</span>
          {canManage ? (
            <MemberPicker
              wsId={wsId}
              mode="single"
              selectedIds={skill.owner_id ? [skill.owner_id] : []}
              onToggle={(id) => ownerMutation.mutate(id)}
              trigger={
                <button
                  type="button"
                  disabled={busy}
                  className="inline-flex items-center gap-1.5 rounded-md border px-2 py-1 hover:bg-accent disabled:opacity-50"
                >
                  <MemberChip
                    userId={skill.owner_id}
                    wsId={wsId}
                    fallback="Vælg ejer"
                  />
                </button>
              }
            />
          ) : (
            <MemberChip userId={skill.owner_id} wsId={wsId} fallback="Ingen ejer" />
          )}
        </div>

        <div className="space-y-1.5">
          <div className="flex items-center justify-between">
            <span className="text-muted-foreground">Godkendere</span>
            {canManage && (
              <MemberPicker
                wsId={wsId}
                mode="multi"
                selectedIds={skill.approver_ids}
                onToggle={toggleApprover}
                trigger={
                  <button
                    type="button"
                    disabled={busy}
                    aria-label="Tilføj godkender"
                    className="inline-flex h-6 w-6 items-center justify-center rounded-md text-muted-foreground hover:bg-accent disabled:opacity-50"
                  >
                    <Plus className="h-3.5 w-3.5" />
                  </button>
                }
              />
            )}
          </div>
          {skill.approver_ids.length === 0 ? (
            <div className="rounded-md border border-dashed px-2 py-2 text-center text-muted-foreground">
              Ingen godkendere endnu.
            </div>
          ) : (
            <ul className="space-y-1">
              {skill.approver_ids.map((id) => (
                <li
                  key={id}
                  className="flex items-center gap-2 rounded-md border bg-card px-2 py-1"
                >
                  <MemberChip userId={id} wsId={wsId} />
                  {canManage && (
                    <button
                      type="button"
                      onClick={() => toggleApprover(id)}
                      disabled={busy}
                      aria-label="Fjern godkender"
                      className="ml-auto text-muted-foreground hover:text-destructive disabled:opacity-50"
                    >
                      <X className="h-3 w-3" />
                    </button>
                  )}
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Versions section
// ---------------------------------------------------------------------------

const versionSelectClass =
  "h-7 rounded-md border bg-card px-1.5 font-mono text-xs focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring";

function VersionsSection({ skill, wsId }: { skill: Skill; wsId: string }) {
  const timeAgo = useTimeAgo();
  const { data: versions = [], isLoading } = useQuery(
    skillVersionsOptions(wsId, skill.id),
  );
  const [viewing, setViewing] = useState<SkillVersion | null>(null);
  const [comparing, setComparing] = useState(false);
  const [baseId, setBaseId] = useState<string>("");
  const [targetId, setTargetId] = useState<string>("");

  // Versions come back newest-first. Default the compare picker to "previous →
  // latest" so opening it shows the most recent change without extra clicks.
  useEffect(() => {
    if (versions.length >= 2 && (!baseId || !targetId)) {
      setTargetId((prev) => prev || versions[0]!.id);
      setBaseId((prev) => prev || versions[1]!.id);
    }
  }, [versions, baseId, targetId]);

  const base = versions.find((v) => v.id === baseId) ?? null;
  const target = versions.find((v) => v.id === targetId) ?? null;

  return (
    <div>
      <div className="mb-2 flex items-center justify-between">
        <SectionHeading>
          <History className="h-3 w-3" />
          Versioner ({versions.length})
        </SectionHeading>
        {versions.length >= 2 && (
          <Button
            type="button"
            size="xs"
            variant="ghost"
            onClick={() => setComparing((v) => !v)}
            className="text-muted-foreground"
          >
            <GitCompare className="h-3 w-3" />
            {comparing ? "Skjul sammenligning" : "Sammenlign"}
          </Button>
        )}
      </div>

      {comparing && versions.length >= 2 && (
        <div className="mb-2 space-y-2 rounded-md border bg-muted/20 p-2">
          <div className="flex items-center gap-2 text-xs">
            <select
              aria-label="Fra version"
              value={baseId}
              onChange={(e) => setBaseId(e.target.value)}
              className={versionSelectClass}
            >
              {versions.map((v) => (
                <option key={v.id} value={v.id}>
                  {v.version}
                </option>
              ))}
            </select>
            <GitCompare className="h-3 w-3 shrink-0 text-muted-foreground" />
            <select
              aria-label="Til version"
              value={targetId}
              onChange={(e) => setTargetId(e.target.value)}
              className={versionSelectClass}
            >
              {versions.map((v) => (
                <option key={v.id} value={v.id}>
                  {v.version}
                </option>
              ))}
            </select>
          </div>
          {base && target ? (
            <LineDiff before={base.content} after={target.content} />
          ) : (
            <div className="text-xs text-muted-foreground">
              Vælg to versioner at sammenligne.
            </div>
          )}
        </div>
      )}

      {isLoading ? (
        <div className="text-xs text-muted-foreground">Indlæser…</div>
      ) : versions.length === 0 ? (
        <div className="rounded-md border border-dashed px-2 py-2 text-center text-xs text-muted-foreground">
          Ingen versionshistorik endnu.
        </div>
      ) : (
        <ul className="space-y-1">
          {versions.map((v) => (
            <li key={v.id}>
              <button
                type="button"
                onClick={() => setViewing(v)}
                className="flex w-full items-center gap-2 rounded-md border bg-card px-2 py-1.5 text-left text-xs hover:bg-accent"
              >
                <span className="font-mono font-medium">{v.version}</span>
                {v.description && (
                  <span className="min-w-0 flex-1 truncate text-muted-foreground">
                    {v.description}
                  </span>
                )}
                <span className="ml-auto shrink-0 text-muted-foreground">
                  {timeAgo(v.created_at)}
                </span>
              </button>
            </li>
          ))}
        </ul>
      )}

      <Dialog open={!!viewing} onOpenChange={(v) => !v && setViewing(null)}>
        <DialogContent className="sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle className="font-mono">
              Version {viewing?.version}
            </DialogTitle>
            <DialogDescription>
              {viewing?.description || "Skrivebeskyttet øjebliksbillede."}
            </DialogDescription>
          </DialogHeader>
          <div className="max-h-80 overflow-auto rounded-md border bg-muted/20 p-3 font-mono text-xs whitespace-pre-wrap break-all">
            {viewing?.content || "(tomt indhold)"}
          </div>
          {viewing && viewing.files.length > 0 && (
            <div className="space-y-1.5">
              <div className="text-xs font-medium text-muted-foreground">
                Filer ({viewing.files.length})
              </div>
              <ul className="space-y-0.5">
                {viewing.files.map((f) => (
                  <li key={f.path} className="font-mono text-xs text-muted-foreground">
                    {f.path}
                  </li>
                ))}
              </ul>
            </div>
          )}
        </DialogContent>
      </Dialog>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Change requests section
// ---------------------------------------------------------------------------

function ChangeRequestCard({
  cr,
  skill,
  wsId,
  canManage,
  focused = false,
}: {
  cr: SkillChangeRequest;
  skill: Skill;
  wsId: string;
  canManage: boolean;
  focused?: boolean;
}) {
  const timeAgo = useTimeAgo();
  const qc = useQueryClient();
  const [expanded, setExpanded] = useState(focused);
  const [rejecting, setRejecting] = useState(false);
  const [rejectComment, setRejectComment] = useState("");
  const cardRef = useRef<HTMLLIElement>(null);

  // Deep-link target (?cr=): expand the diff and scroll the card into view so
  // an inbox click lands directly on the proposal it points at.
  useEffect(() => {
    if (focused && cardRef.current) {
      setExpanded(true);
      cardRef.current.scrollIntoView({ block: "center" });
    }
  }, [focused]);

  const reviewMutation = useMutation({
    mutationFn: (vars: { action: "approve" | "reject"; comment?: string }) =>
      api.reviewSkillChangeRequest(cr.id, vars),
    onSuccess: (_data, vars) => {
      qc.invalidateQueries({
        queryKey: skillOwnershipKeys.changeRequests(wsId, skill.id),
      });
      if (vars.action === "approve") {
        // Approve merges → skill content/version + version history change.
        qc.invalidateQueries({ queryKey: skillOwnershipKeys.versions(wsId, skill.id) });
        qc.invalidateQueries({ queryKey: [...workspaceKeys.skills(wsId), skill.id] });
        qc.invalidateQueries({ queryKey: workspaceKeys.skills(wsId), exact: true });
      }
      setRejecting(false);
      setRejectComment("");
      toast.success(vars.action === "approve" ? "Forslag godkendt" : "Forslag afvist");
    },
    onError: (err) =>
      toast.error(err instanceof Error ? err.message : "Handling fejlede"),
  });

  const pending = cr.status === "pending";

  return (
    <li
      ref={cardRef}
      className={
        focused
          ? "rounded-md border border-brand bg-card px-2.5 py-2 text-xs ring-1 ring-brand"
          : "rounded-md border bg-card px-2.5 py-2 text-xs"
      }
    >
      <div className="flex items-start gap-2">
        <GitPullRequest className="mt-0.5 h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <span className="truncate font-medium">{cr.title}</span>
            {statusBadge(cr.status)}
          </div>
          <div className="mt-0.5 flex flex-wrap items-center gap-x-1.5 text-muted-foreground">
            <span className="font-mono">
              {cr.base_version} → {cr.proposed_version}
            </span>
            <span aria-hidden>·</span>
            <MemberChip userId={cr.proposed_by} wsId={wsId} />
            <span aria-hidden>·</span>
            <span>{timeAgo(cr.created_at)}</span>
            {cr.work_session_id && (
              <>
                <span aria-hidden>·</span>
                <span
                  className="inline-flex items-center gap-1 font-mono"
                  title={`Foreslået i agent-session ${cr.work_session_id}`}
                >
                  <Terminal className="h-3 w-3" />
                  session {cr.work_session_id.slice(0, 8)}
                </span>
              </>
            )}
          </div>
          {cr.description && (
            <p className="mt-1 text-muted-foreground">{cr.description}</p>
          )}
          {cr.review_comment && (
            <p className="mt-1 italic text-muted-foreground">
              Kommentar: {cr.review_comment}
            </p>
          )}

          <button
            type="button"
            onClick={() => setExpanded((v) => !v)}
            className="mt-1.5 text-muted-foreground underline-offset-2 hover:underline"
          >
            {expanded ? "Skjul ændringer" : "Vis ændringer"}
          </button>
          {expanded && (
            <div className="mt-1.5">
              <LineDiff before={skill.content} after={cr.proposed_content} />
            </div>
          )}

          {pending && canManage && (
            <div className="mt-2">
              {rejecting ? (
                <div className="space-y-1.5">
                  <Textarea
                    value={rejectComment}
                    onChange={(e) => setRejectComment(e.target.value)}
                    rows={2}
                    placeholder="Kommentar (valgfri)"
                    className="resize-none text-xs"
                  />
                  <div className="flex items-center gap-1.5">
                    <Button
                      type="button"
                      size="xs"
                      variant="destructive"
                      disabled={reviewMutation.isPending}
                      onClick={() =>
                        reviewMutation.mutate({
                          action: "reject",
                          comment: rejectComment.trim() || undefined,
                        })
                      }
                    >
                      {reviewMutation.isPending ? (
                        <Loader2 className="h-3 w-3 animate-spin" />
                      ) : (
                        <X className="h-3 w-3" />
                      )}
                      Bekræft afvisning
                    </Button>
                    <Button
                      type="button"
                      size="xs"
                      variant="ghost"
                      onClick={() => {
                        setRejecting(false);
                        setRejectComment("");
                      }}
                    >
                      Fortryd
                    </Button>
                  </div>
                </div>
              ) : (
                <div className="flex items-center gap-1.5">
                  <Button
                    type="button"
                    size="xs"
                    disabled={reviewMutation.isPending}
                    onClick={() => reviewMutation.mutate({ action: "approve" })}
                    title="Godkend og flet forslaget — opdaterer skillen og gemmer en ny version"
                  >
                    {reviewMutation.isPending ? (
                      <Loader2 className="h-3 w-3 animate-spin" />
                    ) : (
                      <GitMerge className="h-3 w-3" />
                    )}
                    Godkend &amp; flet
                  </Button>
                  <Button
                    type="button"
                    size="xs"
                    variant="ghost"
                    onClick={() => setRejecting(true)}
                    className="text-muted-foreground hover:text-destructive"
                  >
                    <X className="h-3 w-3" />
                    Afvis
                  </Button>
                </div>
              )}
            </div>
          )}
        </div>
      </div>
    </li>
  );
}

function ChangeRequestsSection({
  skill,
  wsId,
  canManage,
  focusCrId,
}: {
  skill: Skill;
  wsId: string;
  canManage: boolean;
  focusCrId: string | null;
}) {
  const { data: changeRequests = [], isLoading } = useQuery(
    skillChangeRequestsOptions(wsId, skill.id),
  );
  const [createOpen, setCreateOpen] = useState(false);

  return (
    <div>
      <div className="mb-2 flex items-center justify-between">
        <SectionHeading>
          <GitPullRequest className="h-3 w-3" />
          Forslag ({changeRequests.length})
        </SectionHeading>
        <Button
          type="button"
          size="xs"
          variant="ghost"
          onClick={() => setCreateOpen(true)}
          className="text-muted-foreground"
        >
          <Plus className="h-3 w-3" />
          Foreslå ændring
        </Button>
      </div>
      {isLoading ? (
        <div className="text-xs text-muted-foreground">Indlæser…</div>
      ) : changeRequests.length === 0 ? (
        <div className="rounded-md border border-dashed px-2 py-2 text-center text-xs text-muted-foreground">
          Ingen forslag endnu.
        </div>
      ) : (
        <ul className="space-y-1.5">
          {changeRequests.map((cr) => (
            <ChangeRequestCard
              key={cr.id}
              cr={cr}
              skill={skill}
              wsId={wsId}
              canManage={canManage}
              focused={!!focusCrId && cr.id === focusCrId}
            />
          ))}
        </ul>
      )}

      <CreateChangeRequestDialog
        skill={skill}
        wsId={wsId}
        open={createOpen}
        onOpenChange={setCreateOpen}
      />
    </div>
  );
}

// ---------------------------------------------------------------------------
// Forks section
// ---------------------------------------------------------------------------

function ForksSection({ skill, wsId }: { skill: Skill; wsId: string }) {
  const timeAgo = useTimeAgo();
  const paths = useWorkspacePaths();
  const { data: forks = [], isLoading } = useQuery(
    skillForksOptions(wsId, skill.id),
  );
  const { data: parent } = useQuery(skillForkParentOptions(wsId, skill.id));

  return (
    <div>
      <SectionHeading>
        <GitFork className="h-3 w-3" />
        Slægtskab
      </SectionHeading>

      {/* "Forked from" — present only when this skill is itself a fork. */}
      {parent && (
        <AppLink
          href={paths.skillDetail(parent.parent_skill_id)}
          className="mb-2 flex items-center gap-2 rounded-md border bg-muted/30 px-2 py-1.5 text-xs hover:bg-accent"
        >
          <GitFork className="h-3 w-3 shrink-0 -scale-x-100 text-muted-foreground" />
          <span className="shrink-0 text-muted-foreground">Forket fra</span>
          <span className="min-w-0 flex-1 truncate font-medium">
            {parent.parent_name}
          </span>
          <span className="shrink-0 text-muted-foreground">
            {timeAgo(parent.created_at)}
          </span>
        </AppLink>
      )}

      <div className="mb-1.5 text-xs text-muted-foreground">
        Forks ({forks.length})
      </div>
      {isLoading ? (
        <div className="text-xs text-muted-foreground">Indlæser…</div>
      ) : forks.length === 0 ? (
        <div className="rounded-md border border-dashed px-2 py-2 text-center text-xs text-muted-foreground">
          Ingen forks endnu.
        </div>
      ) : (
        <ul className="space-y-1">
          {forks.map((f: SkillFork) => (
            <li key={f.id}>
              <AppLink
                href={paths.skillDetail(f.forked_skill_id)}
                className="flex items-center gap-2 rounded-md border bg-card px-2 py-1.5 text-xs hover:bg-accent"
              >
                <GitFork className="h-3 w-3 shrink-0 text-muted-foreground" />
                <span className="min-w-0 flex-1 truncate font-medium">
                  {f.forked_name ?? "Fork"}
                </span>
                {f.current_version && (
                  <span className="shrink-0 font-mono text-muted-foreground">
                    {f.current_version}
                  </span>
                )}
                <span className="shrink-0 text-muted-foreground">
                  {timeAgo(f.created_at)}
                </span>
              </AppLink>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Panel
// ---------------------------------------------------------------------------

/**
 * Governance panel for the skill detail page (JEH-216). Renders ownership,
 * version history, change requests, and forks. Behind the
 * `cerebro_skill_ownership` flag — returns null when disabled so the upstream
 * page is unchanged. Manage actions (owner/approver edits, CR review) are gated
 * by `useCanManageSkill`, which mirrors the backend permission check.
 */
export function SkillOwnershipPanel({ skill }: { skill: Skill }) {
  const enabled = useFeatureFlag("cerebro_skill_ownership");
  const wsId = useWorkspaceId();
  const canManage = useCanManageSkill(skill, wsId);
  // `?cr=<id>` deep-link from the inbox notification → focus that proposal.
  const { searchParams } = useNavigation();
  const focusCrId = searchParams.get("cr");
  const currentVersion = useMemo(() => skill.current_version, [skill.current_version]);

  if (!enabled) return null;

  return (
    <div className="flex flex-col gap-5 border-t pt-4">
      <div className="flex items-center justify-between text-xs">
        <span className="font-medium uppercase tracking-wider text-muted-foreground">
          Styring
        </span>
        <span className="font-mono text-muted-foreground">v{currentVersion}</span>
      </div>
      <OwnershipSection skill={skill} wsId={wsId} canManage={canManage} />
      <VersionsSection skill={skill} wsId={wsId} />
      <ChangeRequestsSection
        skill={skill}
        wsId={wsId}
        canManage={canManage}
        focusCrId={focusCrId}
      />
      <ForksSection skill={skill} wsId={wsId} />
    </div>
  );
}
