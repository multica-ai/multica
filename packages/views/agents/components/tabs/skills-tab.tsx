"use client";

import { useState } from "react";
import {
  FileText,
  Loader2,
  Plus,
  RefreshCw,
  Server,
  Trash2,
} from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import type {
  Agent,
  AgentRuntime,
  DisabledRuntimeSkill,
  RuntimeLocalSkillSummary,
} from "@multica/core/types";
import { api, ApiError } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { runtimeCapabilitiesOptions } from "@multica/core/runtimes";
import {
  skillDetailOptions,
  skillListOptions,
  workspaceKeys,
} from "@multica/core/workspace/queries";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Switch } from "@multica/ui/components/ui/switch";
import { cn } from "@multica/ui/lib/utils";
import { SkillAddDialog } from "../skill-add-dialog";
import { useT } from "../../../i18n";

type SelectedSkill =
  | { kind: "workspace"; id: string }
  | { kind: "runtime"; skill: RuntimeLocalSkillSummary }
  | null;

export function SkillsTab({
  agent,
  runtime,
  canEdit = true,
}: {
  agent: Agent;
  runtime: AgentRuntime | null;
  canEdit?: boolean;
}) {
  const { t } = useT("agents");
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const { data: workspaceSkills = [] } = useQuery(skillListOptions(wsId));
  const runtimeId =
    runtime?.runtime_mode === "local" && runtime.status === "online"
      ? runtime.id
      : null;
  const runtimeQuery = useQuery(runtimeCapabilitiesOptions(runtimeId));
  const [busyId, setBusyId] = useState<string | null>(null);
  const [showAdd, setShowAdd] = useState(false);
  const [selected, setSelected] = useState<SelectedSkill>(null);
  const selectedWorkspaceId = selected?.kind === "workspace" ? selected.id : "";
  const detailQuery = useQuery(skillDetailOptions(wsId, selectedWorkspaceId));

  const refreshAgent = async () => {
    await qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
  };

  const handleRemove = async (skillId: string) => {
    setBusyId(skillId);
    try {
      await api.removeAgentSkill(agent.id, skillId);
      await refreshAgent();
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t(($) => $.tab_body.skills.remove_failed_toast),
      );
    } finally {
      setBusyId(null);
    }
  };

  const handleToggle = async (skillId: string, enabled: boolean) => {
    setBusyId(skillId);
    try {
      await api.setAgentSkillEnabled(agent.id, skillId, enabled);
      await refreshAgent();
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t(($) => $.tab_body.skills.toggle_failed_toast),
      );
    } finally {
      setBusyId(null);
    }
  };

  const handleRuntimeToggle = async (
    skill: RuntimeLocalSkillSummary,
    enabled: boolean,
  ) => {
    if (!runtime || !skill.root) return;
    const busyKey = runtimeSkillIdentity(skill);
    setBusyId(busyKey);
    try {
      await api.setAgentRuntimeSkillEnabled(agent.id, {
        runtime_id: runtime.id,
        root: skill.root,
        key: skill.key,
        name: skill.name,
        plugin: skill.plugin,
        enabled,
      });
      await refreshAgent();
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t(($) => $.tab_body.skills.runtime_toggle_failed_toast),
      );
    } finally {
      setBusyId(null);
    }
  };

  const runtimeSkills = runtimeQuery.data?.skills ?? [];

  return (
    <div className="space-y-8">
      <p className="text-sm leading-6 text-muted-foreground">
        {t(($) => $.tab_body.skills.intro)}
      </p>

      <CapabilitySection
        title={t(($) => $.tab_body.skills.assigned_title)}
        description={t(($) => $.tab_body.skills.assigned_hint)}
        action={
          canEdit ? (
            <Button
              variant="outline"
              size="sm"
              onClick={() => setShowAdd(true)}
              disabled={workspaceSkills.length === 0}
            >
              <Plus className="h-3.5 w-3.5" />
              {t(($) => $.tab_body.skills.add_action)}
            </Button>
          ) : null
        }
      >
        {agent.skills.length === 0 ? (
          <EmptyState
            icon={<FileText className="h-6 w-6" />}
            title={t(($) => $.tab_body.skills.empty_title)}
            hint={t(($) => $.tab_body.skills.empty_hint)}
          />
        ) : (
          <ul className="divide-y rounded-lg border bg-surface-raised/40">
            {agent.skills.map((skill) => {
              const enabled = skill.enabled !== false;
              const busy = busyId === skill.id;
              return (
                <li key={skill.id} className="flex items-center gap-3 p-3">
                  <button
                    type="button"
                    onClick={() => setSelected({ kind: "workspace", id: skill.id })}
                    className="flex min-w-0 flex-1 items-center gap-3 rounded-md text-left outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  >
                    <span
                      className={cn(
                        "flex size-9 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground",
                        !enabled && "opacity-50",
                      )}
                    >
                      <FileText className="h-4 w-4" />
                    </span>
                    <span className="min-w-0 flex-1">
                      <span className={cn("block text-sm font-medium", !enabled && "text-muted-foreground")}>
                        {skill.name}
                      </span>
                      <span className="block truncate text-xs text-muted-foreground">
                        {skill.description || t(($) => $.tab_body.skills.no_description)}
                      </span>
                    </span>
                  </button>
                  {canEdit && (
                    <>
                      {busy ? (
                        <Loader2 className="h-4 w-4 animate-spin text-muted-foreground motion-reduce:animate-none" />
                      ) : (
                        <Switch
                          checked={enabled}
                          onCheckedChange={(checked) => handleToggle(skill.id, checked)}
                          aria-label={t(($) => $.tab_body.skills.toggle_aria, {
                            name: skill.name,
                          })}
                        />
                      )}
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        onClick={() => handleRemove(skill.id)}
                        disabled={busyId !== null}
                        aria-label={t(($) => $.tab_body.skills.remove_aria, {
                          name: skill.name,
                        })}
                        className="text-muted-foreground hover:text-destructive"
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </Button>
                    </>
                  )}
                </li>
              );
            })}
          </ul>
        )}
      </CapabilitySection>

      <CapabilitySection
        title={t(($) => $.tab_body.skills.runtime_title)}
        description={t(($) => $.tab_body.skills.runtime_hint, {
          runtime: runtime?.custom_name || runtime?.name || "Runtime",
        })}
        action={
          runtimeId ? (
            <Button
              variant="ghost"
              size="sm"
              onClick={() => runtimeQuery.refetch()}
              disabled={runtimeQuery.isFetching}
            >
              <RefreshCw
                className={cn(
                  "h-3.5 w-3.5",
                  runtimeQuery.isFetching && "animate-spin motion-reduce:animate-none",
                )}
              />
              {t(($) => $.tab_body.skills.refresh_action)}
            </Button>
          ) : null
        }
      >
        {!runtime ? (
          <RuntimeNotice text={t(($) => $.tab_body.skills.runtime_missing)} />
        ) : runtime.status !== "online" ? (
          <RuntimeNotice text={t(($) => $.tab_body.skills.runtime_offline)} />
        ) : runtimeQuery.isLoading ? (
          <RuntimeNotice
            loading
            text={t(($) => $.tab_body.skills.runtime_discovering)}
          />
        ) : runtimeQuery.isError ? (
          <RuntimeNotice
            text={
              runtimeQuery.error instanceof ApiError &&
              runtimeQuery.error.status === 403
                ? t(($) => $.tab_body.skills.runtime_forbidden)
                : t(($) => $.tab_body.skills.runtime_failed)
            }
          />
        ) : runtimeQuery.data?.supported !== true ? (
          <RuntimeNotice text={t(($) => $.tab_body.skills.runtime_unsupported)} />
        ) : runtimeSkills.length === 0 ? (
          <RuntimeNotice text={t(($) => $.tab_body.skills.runtime_empty)} />
        ) : (
          <ul className="divide-y rounded-lg border bg-surface-raised/40">
            {runtimeSkills.map((skill) => {
              const disabled = isRuntimeSkillDisabled(
                agent.disabled_runtime_skills,
                runtime?.id,
                skill,
              );
              const busyKey = runtimeSkillIdentity(skill);
              const busy = busyId === busyKey;
              return (
                <li
                  key={`${skill.root ?? "unknown"}:${skill.key}`}
                  className="flex items-center gap-3 p-3"
                >
                  <button
                    type="button"
                    onClick={() => setSelected({ kind: "runtime", skill })}
                    className="flex min-w-0 flex-1 items-center gap-3 rounded-md text-left outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  >
                    <span
                      className={cn(
                        "flex size-9 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground",
                        disabled && "opacity-50",
                      )}
                    >
                      <Server className="h-4 w-4" />
                    </span>
                    <span className="min-w-0 flex-1">
                      <span
                        className={cn(
                          "block text-sm font-medium",
                          disabled && "text-muted-foreground",
                        )}
                      >
                        {skill.name}
                      </span>
                      <span className="block truncate text-xs text-muted-foreground">
                        {skill.description || skill.source_path}
                      </span>
                    </span>
                  </button>
                  {canEdit &&
                    skill.can_disable === true &&
                    skill.root &&
                    (busy ? (
                      <Loader2 className="h-4 w-4 animate-spin text-muted-foreground motion-reduce:animate-none" />
                    ) : (
                      <Switch
                        checked={!disabled}
                        onCheckedChange={(checked) =>
                          handleRuntimeToggle(skill, checked)
                        }
                        aria-label={t(
                          ($) => $.tab_body.skills.runtime_toggle_aria,
                          { name: skill.name },
                        )}
                      />
                    ))}
                </li>
              );
            })}
          </ul>
        )}
      </CapabilitySection>

      <SkillAddDialog agent={agent} open={showAdd} onOpenChange={setShowAdd} />
      <SkillDetailDialog
        selected={selected}
        onOpenChange={(open) => !open && setSelected(null)}
        workspaceSkill={detailQuery.data}
        loading={selected?.kind === "workspace" && detailQuery.isLoading}
      />
    </div>
  );
}

function runtimeSkillIdentity(skill: RuntimeLocalSkillSummary): string {
  return `runtime:${skill.root ?? "unknown"}:${skill.key}:${skill.plugin ?? ""}`;
}

function isRuntimeSkillDisabled(
  disabledSkills: DisabledRuntimeSkill[] | undefined,
  runtimeId: string | undefined,
  skill: RuntimeLocalSkillSummary,
): boolean {
  if (!runtimeId || !skill.root) return false;
  return (disabledSkills ?? []).some(
    (disabled) =>
      disabled.runtime_id === runtimeId &&
      disabled.provider === skill.provider &&
      disabled.root === skill.root &&
      disabled.key === skill.key &&
      (disabled.plugin ?? "") === (skill.plugin ?? ""),
  );
}

function CapabilitySection({
  title,
  description,
  action,
  children,
}: {
  title: string;
  description: string;
  action?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <section className="space-y-3">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h3 className="text-sm font-medium">{title}</h3>
          <p className="mt-1 text-xs leading-5 text-muted-foreground">{description}</p>
        </div>
        {action}
      </div>
      {children}
    </section>
  );
}

function EmptyState({ icon, title, hint }: { icon: React.ReactNode; title: string; hint: string }) {
  return (
    <div className="flex flex-col items-center justify-center rounded-lg border border-dashed py-10 text-muted-foreground">
      <span className="opacity-50">{icon}</span>
      <p className="mt-3 text-sm">{title}</p>
      <p className="mt-1 max-w-sm text-center text-xs">{hint}</p>
    </div>
  );
}

function RuntimeNotice({ text, loading = false }: { text: string; loading?: boolean }) {
  return (
    <div className="flex items-center gap-2 rounded-lg border border-dashed px-4 py-6 text-xs text-muted-foreground">
      {loading ? (
        <Loader2 className="h-4 w-4 animate-spin motion-reduce:animate-none" />
      ) : (
        <Server className="h-4 w-4" />
      )}
      {text}
    </div>
  );
}

function SkillDetailDialog({
  selected,
  onOpenChange,
  workspaceSkill,
  loading,
}: {
  selected: SelectedSkill;
  onOpenChange: (open: boolean) => void;
  workspaceSkill?: Awaited<ReturnType<typeof api.getSkill>>;
  loading: boolean;
}) {
  const { t } = useT("agents");
  const runtimeSkill = selected?.kind === "runtime" ? selected.skill : null;
  const title = runtimeSkill?.name || workspaceSkill?.name || t(($) => $.tab_body.skills.detail_title);
  const description = runtimeSkill?.description || workspaceSkill?.description;

  return (
    <Dialog open={selected !== null} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[80vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <div className="flex items-center gap-2 pr-8">
            <DialogTitle>{title}</DialogTitle>
            <Badge variant={runtimeSkill ? "secondary" : "outline"}>
              {runtimeSkill
                ? t(($) => $.tab_body.skills.inherited_badge)
                : t(($) => $.tab_body.skills.workspace_badge)}
            </Badge>
          </div>
          {description && <DialogDescription>{description}</DialogDescription>}
        </DialogHeader>
        {loading ? (
          <div className="flex items-center gap-2 py-8 text-muted-foreground">
            <Loader2 className="h-4 w-4 animate-spin motion-reduce:animate-none" />
            {t(($) => $.tab_body.skills.detail_loading)}
          </div>
        ) : runtimeSkill ? (
          <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-3 rounded-lg border p-4 text-xs">
            <dt className="text-muted-foreground">{t(($) => $.tab_body.skills.detail_source)}</dt>
            <dd className="break-all">{runtimeSkill.source_path}</dd>
            <dt className="text-muted-foreground">{t(($) => $.tab_body.skills.detail_provider)}</dt>
            <dd>{runtimeSkill.provider}</dd>
            {runtimeSkill.plugin && (
              <>
                <dt className="text-muted-foreground">{t(($) => $.tab_body.skills.detail_plugin)}</dt>
                <dd>{runtimeSkill.plugin}</dd>
              </>
            )}
            <dt className="text-muted-foreground">{t(($) => $.tab_body.skills.detail_files)}</dt>
            <dd>{runtimeSkill.file_count}</dd>
          </dl>
        ) : workspaceSkill ? (
          <div className="space-y-4">
            <div className="max-h-96 overflow-auto rounded-lg border bg-muted/30 p-4">
              <pre className="whitespace-pre-wrap break-words font-mono text-xs leading-5">
                {workspaceSkill.content}
              </pre>
            </div>
            {(workspaceSkill.files ?? []).length > 0 && (
              <div>
                <h4 className="text-xs font-medium">{t(($) => $.tab_body.skills.detail_supporting_files)}</h4>
                <div className="mt-2 flex flex-wrap gap-2">
                  {(workspaceSkill.files ?? []).map((file) => (
                    <Badge key={file.id} variant="outline">{file.path}</Badge>
                  ))}
                </div>
              </div>
            )}
          </div>
        ) : null}
      </DialogContent>
    </Dialog>
  );
}
