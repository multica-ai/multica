"use client";

import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowLeft,
  Bot,
  Check,
  ChevronRight,
  FileText,
  Loader2,
  MessageSquare,
  Search,
} from "lucide-react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { useFeatureEnabled } from "@multica/core/config";
import { AGENT_BUILDER_FLAG } from "@multica/core/feature-flags";
import {
  agentTemplateDetailOptions,
  agentTemplateListOptions,
} from "@multica/core/agents";
import {
  chatKeys,
  chatMessagesOptions,
  pendingChatTaskOptions,
} from "@multica/core/chat/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import {
  runtimeListOptions,
  runtimeModelsOptions,
} from "@multica/core/runtimes";
import type {
  Agent,
  AgentInvocationTargetInput,
  AgentTemplateSummary,
  ChatMessage,
  CreateAgentRequest,
  MemberWithUser,
  RuntimeDevice,
  RuntimeModel,
} from "@multica/core/types";
import {
  agentListOptions,
  memberListOptions,
  skillListOptions,
  workspaceKeys,
} from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { cn } from "@multica/ui/lib/utils";
import { AvatarUploadControl } from "../../common/avatar-upload-control";
import { ChatInput } from "../../chat/components/chat-input";
import {
  ChatMessageList,
  ChatMessageSkeleton,
} from "../../chat/components/chat-message-list";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import { ActorAvatar } from "../../common/actor-avatar";
import {
  SettingsCard,
  SettingsSection,
} from "../../settings/components/settings-layout";
import { ModelDropdown } from "./model-dropdown";
import { RuntimePicker, isRuntimeUsableForUser } from "./runtime-picker";
import { SkillMultiSelect } from "./skill-multi-select";

type StudioMode = "choose" | "templates" | "blank" | "template" | "ai";
type PermissionScope = "private" | "workspace" | "members";

export interface AgentDraft {
  name: string;
  description: string;
  instructions: string;
  avatarUrl: string | null;
  runtimeId: string;
  model: string;
  skillIds: Set<string>;
  permissionScope: PermissionScope;
  memberIds: Set<string>;
  /** Team grants are not editable in this form yet, but duplicates must preserve them. */
  teamIds: Set<string>;
}

export interface BuilderDraftPayload {
  name?: unknown;
  description?: unknown;
  instructions?: unknown;
  model?: unknown;
  skill_ids?: unknown;
  permission_scope?: unknown;
  member_ids?: unknown;
}

const BUILDER_INPUT_PREFIX = "MULTICA_AGENT_BUILDER_INPUT\n";
const EMPTY_CHAT_MESSAGES: ChatMessage[] = [];
const EMPTY_DRAFT: AgentDraft = {
  name: "",
  description: "",
  instructions: "",
  avatarUrl: null,
  runtimeId: "",
  model: "",
  skillIds: new Set(),
  permissionScope: "private",
  memberIds: new Set(),
  teamIds: new Set(),
};

export function AgentCreationStudio() {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const qc = useQueryClient();
  const currentUser = useAuthStore((state) => state.user);
  const agentBuilderEnabled = useFeatureEnabled(AGENT_BUILDER_FLAG, false);
  const duplicateId = navigation.searchParams.get("duplicate");
  const squadId = navigation.searchParams.get("squad");

  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: runtimes = [], isLoading: runtimesLoading } = useQuery(
    runtimeListOptions(wsId),
  );
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: workspaceSkills = [] } = useQuery(skillListOptions(wsId));
  const { data: templates = [], isLoading: templatesLoading } = useQuery(
    agentTemplateListOptions(),
  );

  const duplicateAgent = duplicateId
    ? agents.find((agent) => agent.id === duplicateId) ?? null
    : null;
  const [mode, setMode] = useState<StudioMode>(duplicateId ? "blank" : "choose");
  const [draft, setDraft] = useState<AgentDraft>(EMPTY_DRAFT);
  const [sourceTemplate, setSourceTemplate] = useState<AgentTemplateSummary | null>(null);
  const [selectedTemplate, setSelectedTemplate] = useState<AgentTemplateSummary | null>(null);
  const [templateSearch, setTemplateSearch] = useState("");
  const [creating, setCreating] = useState(false);
  const [createError, setCreateError] = useState<string | null>(null);
  const [builderSessionId, setBuilderSessionId] = useState("");
  const [builderStarting, setBuilderStarting] = useState(false);
  const [builderClosing, setBuilderClosing] = useState(false);
  const [builderError, setBuilderError] = useState<string | null>(null);
  const [builderRestoreDraft, setBuilderRestoreDraft] = useState<{
    id: string;
    content: string;
  } | null>(null);
  const duplicateAppliedRef = useRef(false);
  const appliedAssistantMessageRef = useRef<string | null>(null);
  const builderSessionIdRef = useRef("");

  useEffect(() => {
    builderSessionIdRef.current = builderSessionId;
  }, [builderSessionId]);

  useEffect(
    () => () => {
      const sessionId = builderSessionIdRef.current;
      if (sessionId) {
        // Covers route/sidebar navigation that bypasses the Studio back button.
        // The endpoint atomically cancels any running task before deletion.
        void api.deleteChatSession(sessionId).catch(() => {
          // A hard page unload may interrupt cleanup; runtime teardown remains
          // the final safety net for an orphaned hidden Builder Agent.
        });
      }
    },
    [],
  );

  const hasUnsavedDraft =
    mode !== "choose" &&
    mode !== "templates" &&
    (builderSessionId.length > 0 ||
      draft.name.trim().length > 0 ||
      draft.description.trim().length > 0 ||
      draft.instructions.trim().length > 0 ||
      draft.skillIds.size > 0);

  useEffect(() => {
    if (!hasUnsavedDraft || creating) return;
    const warnBeforeUnload = (event: BeforeUnloadEvent) => {
      event.preventDefault();
      event.returnValue = "";
    };
    window.addEventListener("beforeunload", warnBeforeUnload);
    return () => window.removeEventListener("beforeunload", warnBeforeUnload);
  }, [creating, hasUnsavedDraft]);

  const templateSlug = selectedTemplate?.slug ?? "";
  const templateDetailQuery = useQuery({
    ...agentTemplateDetailOptions(templateSlug),
    enabled: templateSlug.length > 0,
  });

  const builderMessagesQuery = useQuery({
    ...chatMessagesOptions(builderSessionId),
    refetchInterval: builderSessionId ? 1500 : false,
  });
  const builderPendingQuery = useQuery({
    ...pendingChatTaskOptions(builderSessionId),
    refetchInterval: builderSessionId ? 1500 : false,
  });
  const builderMessages = builderMessagesQuery.data ?? EMPTY_CHAT_MESSAGES;
  const builderPending = !!builderPendingQuery.data?.task_id;
  const builderDisplayMessages = useMemo(
    () =>
      builderMessages.map((message) => ({
        ...message,
        content:
          message.role === "user"
            ? decodeBuilderInput(message.content)
            : stripBuilderDraft(message.content),
      })),
    [builderMessages],
  );

  const usableRuntimes = useMemo(
    () =>
      runtimes.filter(
        (runtime) =>
          runtime.status === "online" &&
          isRuntimeUsableForUser(runtime, currentUser?.id ?? null),
      ),
    [currentUser?.id, runtimes],
  );
  const selectedRuntime =
    runtimes.find((runtime) => runtime.id === draft.runtimeId) ?? null;
  const builderModelsQuery = useQuery(
    runtimeModelsOptions(
      mode === "ai" && selectedRuntime?.status === "online"
        ? selectedRuntime.id
        : null,
    ),
  );
  // `null` means discovery is not available yet (or failed), while `[]` is
  // an authoritative catalog with no selectable models. In both cases the
  // builder may preserve the user's current value but cannot invent one.
  const builderModelCatalog = useMemo(
    () =>
      builderModelsQuery.isSuccess
        ? builderModelsQuery.data.supported
          ? builderModelsQuery.data.models
          : []
        : null,
    [builderModelsQuery.data, builderModelsQuery.isSuccess],
  );
  const validBuilderModelIds = useMemo(
    () =>
      builderModelCatalog === null
        ? null
        : new Set(builderModelCatalog.map((model) => model.id)),
    [builderModelCatalog],
  );

  useEffect(() => {
    if (draft.runtimeId || usableRuntimes.length === 0) return;
    setDraft((current) => ({ ...current, runtimeId: usableRuntimes[0]?.id ?? "" }));
  }, [draft.runtimeId, usableRuntimes]);

  useEffect(() => {
    if (!duplicateAgent || duplicateAppliedRef.current) return;
    duplicateAppliedRef.current = true;
    const duplicateAccess = deriveDuplicateAccess(duplicateAgent);
    setDraft({
      name: `${duplicateAgent.name}${t(($) => $.create_dialog.duplicate_copy_suffix)}`,
      description: duplicateAgent.description ?? "",
      instructions: duplicateAgent.instructions ?? "",
      avatarUrl: duplicateAgent.avatar_url ?? null,
      runtimeId:
        duplicateAgent.runtime_id &&
        runtimes.some(
          (runtime) =>
            runtime.id === duplicateAgent.runtime_id &&
            isRuntimeUsableForUser(runtime, currentUser?.id ?? null),
        )
          ? duplicateAgent.runtime_id
          : usableRuntimes[0]?.id ?? "",
      model: duplicateAgent.model ?? "",
      skillIds: new Set(duplicateAgent.skills.map((skill) => skill.id)),
      ...duplicateAccess,
    });
  }, [currentUser?.id, duplicateAgent, runtimes, t, usableRuntimes]);

  const skillIdSet = useMemo(
    () => new Set(workspaceSkills.map((skill) => skill.id)),
    [workspaceSkills],
  );
  const memberIdSet = useMemo(
    () => new Set(members.map((member) => member.user_id)),
    [members],
  );
  // Realtime chat updates can mutate the cached messages array in place. Use
  // the latest structured message's scalar identity/content as effect inputs
  // so a draft is still applied when the array reference itself is unchanged.
  const latestBuilderDraftMessage = [...builderMessages]
    .reverse()
    .find(
      (message) =>
        message.role === "assistant" && parseBuilderDraft(message.content),
    );
  const latestBuilderDraftMessageId = latestBuilderDraftMessage?.id;
  const latestBuilderDraftMessageContent = latestBuilderDraftMessage?.content;

  useEffect(() => {
    if (
      !latestBuilderDraftMessageId ||
      !latestBuilderDraftMessageContent ||
      latestBuilderDraftMessageId === appliedAssistantMessageRef.current
    ) {
      return;
    }
    const payload = parseBuilderDraft(latestBuilderDraftMessageContent);
    if (!payload) return;
    appliedAssistantMessageRef.current = latestBuilderDraftMessageId;
    setDraft((current) =>
      mergeBuilderDraft(
        current,
        payload,
        skillIdSet,
        memberIdSet,
        validBuilderModelIds,
      ),
    );
  }, [
    latestBuilderDraftMessageContent,
    latestBuilderDraftMessageId,
    memberIdSet,
    skillIdSet,
    validBuilderModelIds,
  ]);

  const filteredTemplates = useMemo(() => {
    const query = templateSearch.trim().toLowerCase();
    if (!query) return templates;
    return templates.filter(
      (template) =>
        template.name.toLowerCase().includes(query) ||
        template.description.toLowerCase().includes(query) ||
        template.category?.toLowerCase().includes(query),
    );
  }, [templateSearch, templates]);

  const accessInvalid =
    draft.permissionScope === "members" &&
    draft.memberIds.size === 0 &&
    draft.teamIds.size === 0;
  const canCreate =
    draft.name.trim().length > 0 &&
    selectedRuntime != null &&
    isRuntimeUsableForUser(selectedRuntime, currentUser?.id ?? null) &&
    !accessInvalid &&
    !creating;
  const currentModeLabel =
    mode === "choose"
      ? t(($) => $.creation_studio.step_choose)
      : mode === "templates"
        ? t(($) => $.creation_studio.step_template)
        : mode === "ai"
          ? t(($) => $.creation_studio.step_ai)
          : t(($) => $.creation_studio.step_configure);

  const resetCreationMode = () => {
    setMode("choose");
    setSelectedTemplate(null);
    setSourceTemplate(null);
    setBuilderSessionId("");
  };

  const deleteBuilderSession = async () => {
    if (!builderSessionId) return true;
    setBuilderClosing(true);
    setBuilderError(null);
    try {
      await api.deleteChatSession(builderSessionId);
      qc.removeQueries({ queryKey: chatKeys.messages(builderSessionId) });
      qc.removeQueries({ queryKey: chatKeys.pendingTask(builderSessionId) });
      builderSessionIdRef.current = "";
      setBuilderSessionId("");
      return true;
    } catch (error) {
      setBuilderError(
        error instanceof Error
          ? error.message
          : t(($) => $.creation_studio.builder.stop_failed),
      );
      return false;
    } finally {
      setBuilderClosing(false);
    }
  };

  const goBack = async () => {
    if (mode === "choose") {
      navigation.push(paths.agents());
      return;
    }
    if (mode === "templates" && selectedTemplate) {
      setSelectedTemplate(null);
      return;
    }
    if (duplicateId) {
      if (!(await deleteBuilderSession())) return;
      navigation.push(paths.agents());
      return;
    }
    if (!(await deleteBuilderSession())) return;
    resetCreationMode();
  };

  const chooseBlank = () => {
    setSourceTemplate(null);
    setDraft((current) => ({
      ...EMPTY_DRAFT,
      runtimeId: current.runtimeId || usableRuntimes[0]?.id || "",
    }));
    setMode("blank");
  };

  const applyTemplate = () => {
    const detail = templateDetailQuery.data;
    if (!selectedTemplate || !detail) return;
    setSourceTemplate(selectedTemplate);
    setDraft((current) => ({
      ...EMPTY_DRAFT,
      name: detail.name,
      description: detail.description,
      instructions: detail.instructions,
      runtimeId: current.runtimeId || usableRuntimes[0]?.id || "",
    }));
    setMode("template");
  };

  const startBuilder = async () => {
    if (!selectedRuntime || selectedRuntime.status !== "online") return;
    setBuilderStarting(true);
    setBuilderError(null);
    try {
      const session = await api.createAgentBuilderSession({
        runtime_id: selectedRuntime.id,
        model: draft.model.trim() || undefined,
      });
      if (!session.session_id) throw new Error(t(($) => $.creation_studio.builder.start_failed));
      setBuilderSessionId(session.session_id);
    } catch (error) {
      setBuilderError(
        error instanceof Error ? error.message : t(($) => $.creation_studio.builder.start_failed),
      );
    } finally {
      setBuilderStarting(false);
    }
  };

  const sendBuilderMessage = async (content: string): Promise<boolean> => {
    const text = content.trim();
    if (!text || !builderSessionId || builderPending) return false;
    setBuilderError(null);
    try {
      const encodedContent = encodeBuilderInput(
        text,
        draft,
        workspaceSkills,
        members,
        selectedRuntime,
        builderModelCatalog,
      );
      const result = await api.sendChatMessage(
        builderSessionId,
        encodedContent,
      );
      const createdAt = new Date().toISOString();
      qc.setQueryData<ChatMessage[]>(
        chatKeys.messages(builderSessionId),
        (current = []) =>
          current.some((message) => message.id === result.message_id)
            ? current
            : [
                ...current,
                {
                  id: result.message_id,
                  chat_session_id: builderSessionId,
                  role: "user",
                  content: encodedContent,
                  task_id: result.task_id,
                  created_at: createdAt,
                },
              ],
      );
      qc.setQueryData(chatKeys.pendingTask(builderSessionId), {
        task_id: result.task_id,
        status: "queued",
        created_at: createdAt,
      });
      await Promise.all([
        qc.invalidateQueries({ queryKey: chatKeys.messages(builderSessionId) }),
        qc.invalidateQueries({ queryKey: chatKeys.pendingTask(builderSessionId) }),
      ]);
      return true;
    } catch (error) {
      setBuilderError(
        error instanceof Error ? error.message : t(($) => $.creation_studio.builder.send_failed),
      );
      return false;
    }
  };

  const stopBuilder = async () => {
    const taskId = builderPendingQuery.data?.task_id;
    if (!taskId || !builderSessionId) return;
    qc.setQueryData(chatKeys.pendingTask(builderSessionId), {});
    try {
      const result = await api.cancelTaskById(taskId);
      const restored = result.cancelled_chat_message;
      if (restored?.restore_to_input) {
        setBuilderRestoreDraft({
          id: restored.message_id,
          content: decodeBuilderInput(restored.content),
        });
      }
      await Promise.all([
        qc.invalidateQueries({ queryKey: chatKeys.messages(builderSessionId) }),
        qc.invalidateQueries({ queryKey: chatKeys.pendingTask(builderSessionId) }),
      ]);
    } catch (error) {
      setBuilderError(
        error instanceof Error
          ? error.message
          : t(($) => $.creation_studio.builder.stop_failed),
      );
      qc.invalidateQueries({ queryKey: chatKeys.pendingTask(builderSessionId) });
    }
  };

  const createAgent = async () => {
    if (!canCreate || !selectedRuntime) return;
    setCreating(true);
    setCreateError(null);
    try {
      const invocationTargets = buildInvocationTargets(draft);
      let agent: Agent;
      if (sourceTemplate) {
        const response = await api.createAgentFromTemplate({
          template_slug: sourceTemplate.slug,
          name: draft.name.trim(),
          description: draft.description.trim(),
          instructions: draft.instructions.trim(),
          avatar_url: draft.avatarUrl ?? undefined,
          runtime_id: selectedRuntime.id,
          model: draft.model.trim() || undefined,
          permission_mode:
            draft.permissionScope === "private" ? "private" : "public_to",
          invocation_targets: invocationTargets,
          extra_skill_ids: [...draft.skillIds],
        });
        agent = response.agent;
      } else {
        const request: CreateAgentRequest = {
          name: draft.name.trim(),
          description: draft.description.trim(),
          instructions: draft.instructions.trim() || undefined,
          avatar_url: draft.avatarUrl ?? undefined,
          runtime_id: selectedRuntime.id,
          model: draft.model.trim() || undefined,
          permission_mode:
            draft.permissionScope === "private" ? "private" : "public_to",
          invocation_targets: invocationTargets,
          skill_ids: [...draft.skillIds],
          template: mode === "ai" ? "agent_builder" : undefined,
        };
        if (duplicateAgent) {
          if (duplicateAgent.custom_args.length > 0) {
            request.custom_args = duplicateAgent.custom_args;
          }
          request.max_concurrent_tasks = duplicateAgent.max_concurrent_tasks;
        }
        agent = await api.createAgent(request);
      }

      if (!agent.id) throw new Error(t(($) => $.creation_studio.create_failed));
      if (squadId) {
        try {
          await api.addSquadMember(squadId, {
            member_type: "agent",
            member_id: agent.id,
          });
          await Promise.all([
            qc.invalidateQueries({
              queryKey: [...workspaceKeys.squads(wsId), squadId, "members"],
            }),
            qc.invalidateQueries({
              queryKey: [...workspaceKeys.squads(wsId), squadId],
            }),
          ]);
        } catch (error) {
          toast.warning(
            t(($) => $.create_dialog.squad_join_failed_toast, {
              name: agent.name || draft.name.trim(),
              error: error instanceof Error ? error.message : "unknown error",
            }),
          );
        }
      }
      if (builderSessionId) {
        // The Agent is already committed. Builder cleanup is best-effort and
        // must never turn a successful create into a retryable create error.
        try {
          await api.deleteChatSession(builderSessionId);
          builderSessionIdRef.current = "";
        } catch {
          // Runtime teardown also removes orphaned system builders.
        }
      }
      await qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
      toast.success(t(($) => $.creation_studio.created, { name: agent.name || draft.name.trim() }));
      navigation.push(squadId ? paths.squadDetail(squadId) : paths.agentDetail(agent.id));
    } catch (error) {
      setCreateError(
        error instanceof Error ? error.message : t(($) => $.creation_studio.create_failed),
      );
      setCreating(false);
    }
  };

  return (
    <div className="flex min-h-0 flex-1 flex-col bg-background">
      <header className="flex h-14 shrink-0 items-center gap-3 border-b px-5">
        <Button type="button" variant="ghost" size="icon" onClick={() => void goBack()} disabled={builderClosing} aria-label={t(($) => $.creation_studio.back)}>
          <ArrowLeft className="size-4" aria-hidden="true" />
        </Button>
        <div className="min-w-0">
          <h1 className="truncate text-sm font-semibold">
            {duplicateAgent
              ? t(($) => $.creation_studio.duplicate_title, { name: duplicateAgent.name })
              : squadId
                ? t(($) => $.creation_studio.squad_title)
                : t(($) => $.creation_studio.title)}
          </h1>
          <p className="truncate text-xs text-muted-foreground">
            {currentModeLabel}
          </p>
        </div>
        {mode !== "choose" && mode !== "templates" && (
          <div className="ml-auto hidden items-center gap-2 text-xs text-muted-foreground sm:flex">
            <span className="rounded-full bg-muted px-2 py-1">
              {sourceTemplate?.name ?? (mode === "ai" ? t(($) => $.creation_studio.modes.ai.title) : t(($) => $.creation_studio.modes.blank.title))}
            </span>
            {selectedRuntime && (
              <span className="rounded-full bg-muted px-2 py-1">
                {selectedRuntime.name || selectedRuntime.provider}
              </span>
            )}
          </div>
        )}
      </header>

      {mode === "choose" && (
        <ModeChooser
          onBlank={chooseBlank}
          onAI={() => setMode("ai")}
          agentBuilderEnabled={agentBuilderEnabled}
        />
      )}

      {mode === "templates" && (
        <TemplateChooser
          templates={filteredTemplates}
          loading={templatesLoading}
          search={templateSearch}
          onSearch={setTemplateSearch}
          selected={selectedTemplate}
          onSelect={setSelectedTemplate}
          detail={templateDetailQuery.data}
          detailLoading={templateDetailQuery.isLoading}
          onUse={applyTemplate}
        />
      )}

      {(mode === "blank" || mode === "template") && (
        <div className="min-h-0 flex-1 overflow-y-auto">
          <div className="mx-auto w-full max-w-4xl px-5 py-8 sm:px-8">
            {duplicateAgent && (
              <div className="mb-5 rounded-lg border border-warning/30 bg-warning/5 px-4 py-3 text-sm">
                {t(($) => $.creation_studio.duplicate_env_notice)}
              </div>
            )}
            <ConfigurationPanel
              draft={draft}
              onChange={setDraft}
              runtimes={runtimes}
              runtimesLoading={runtimesLoading}
              members={members}
              currentUserId={currentUser?.id ?? null}
              createError={createError}
            />
          </div>
          <StudioFooter
            canCreate={canCreate}
            creating={creating}
            squad={!!squadId}
            onCreate={createAgent}
          />
        </div>
      )}

      {mode === "ai" && !builderSessionId && (
        <BuilderSetup
          draft={draft}
          onChange={setDraft}
          runtimes={runtimes}
          runtimesLoading={runtimesLoading}
          members={members}
          currentUserId={currentUser?.id ?? null}
          selectedRuntime={selectedRuntime}
          starting={builderStarting}
          error={builderError}
          onStart={startBuilder}
          onConnectRuntime={() => navigation.push(paths.runtimes())}
        />
      )}

      {mode === "ai" && builderSessionId && (
        <div className="grid min-h-0 flex-1 grid-cols-1 lg:grid-cols-[minmax(0,1.1fr)_minmax(420px,0.9fr)]">
          <BuilderConversation
            sessionId={builderSessionId}
            messages={builderDisplayMessages}
            loading={builderMessagesQuery.isLoading}
            pendingTask={builderPendingQuery.data}
            runtimeOnline={selectedRuntime?.status === "online"}
            onSend={sendBuilderMessage}
            onStop={() => void stopBuilder()}
            restoreDraftRequest={builderRestoreDraft}
            onRestoreDraftConsumed={() => setBuilderRestoreDraft(null)}
            error={builderError}
          />
          <div className="min-h-0 overflow-y-auto border-l bg-muted/10">
            <div className="mx-auto max-w-2xl px-5 py-6">
              <div className="mb-6">
                <h2 className="text-base font-semibold tracking-tight">
                  {t(($) => $.creation_studio.live_draft)}
                </h2>
                <p className="mt-1 text-xs text-muted-foreground">
                  {t(($) => $.creation_studio.live_draft_hint)}
                </p>
              </div>
              <ConfigurationPanel
                compact
                draft={draft}
                onChange={setDraft}
                runtimes={runtimes}
                runtimesLoading={runtimesLoading}
                members={members}
                currentUserId={currentUser?.id ?? null}
                createError={createError}
              />
            </div>
            <StudioFooter
              canCreate={canCreate && !builderPending}
              creating={creating}
              squad={!!squadId}
              onCreate={createAgent}
            />
          </div>
        </div>
      )}
    </div>
  );
}

function ModeChooser({
  onBlank,
  onAI,
  agentBuilderEnabled,
}: {
  onBlank: () => void;
  onAI: () => void;
  agentBuilderEnabled: boolean;
}) {
  const { t } = useT("agents");
  const modes = [
    {
      icon: FileText,
      title: t(($) => $.creation_studio.modes.blank.title),
      description: t(($) => $.creation_studio.modes.blank.description),
      action: onBlank,
    },
    ...(agentBuilderEnabled
      ? [{
          icon: MessageSquare,
          title: t(($) => $.creation_studio.modes.ai.title),
          description: t(($) => $.creation_studio.modes.ai.description),
          action: onAI,
          recommended: true,
        }]
      : []),
  ];
  return (
    <main className="flex min-h-0 flex-1 items-center justify-center overflow-y-auto px-5 py-10">
      <div className="w-full max-w-5xl">
        <div className="mx-auto max-w-2xl text-center">
          <div className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            {t(($) => $.creation_studio.eyebrow)}
          </div>
          <h2 className="mt-2 text-balance text-2xl font-semibold tracking-tight sm:text-3xl">
            {t(($) => $.creation_studio.choose_title)}
          </h2>
          <p className="mt-3 text-pretty text-sm text-muted-foreground">
            {t(($) => $.creation_studio.choose_description)}
          </p>
        </div>
        <div className="mx-auto mt-9 grid max-w-3xl gap-4 md:grid-cols-2">
          {modes.map(({ icon: Icon, title, description, action, recommended }) => (
            <button
              key={title}
              type="button"
              onClick={action}
              className={cn(
                "group relative flex min-h-56 flex-col items-start rounded-xl border bg-card p-5 text-left",
                "transition-[border-color,background-color,transform] hover:-translate-y-0.5 hover:border-primary/40 hover:bg-accent/30",
                "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                recommended && "border-primary/30 bg-primary/[0.025]",
              )}
            >
              {recommended && (
                <span className="absolute right-4 top-4 rounded-full bg-primary/10 px-2 py-1 text-[10px] font-medium text-primary">
                  {t(($) => $.creation_studio.recommended)}
                </span>
              )}
              <span className="flex size-11 items-center justify-center rounded-lg bg-muted text-muted-foreground group-hover:text-foreground">
                <Icon className="size-5" aria-hidden="true" />
              </span>
              <span className="mt-7 text-base font-semibold">{title}</span>
              <span className="mt-2 text-sm leading-6 text-muted-foreground">{description}</span>
              <span className="mt-auto flex items-center gap-1 pt-5 text-xs font-medium text-foreground">
                {t(($) => $.creation_studio.continue)}
                <ChevronRight className="size-3.5 transition-transform group-hover:translate-x-0.5" aria-hidden="true" />
              </span>
            </button>
          ))}
        </div>
      </div>
    </main>
  );
}

function TemplateChooser({
  templates,
  loading,
  search,
  onSearch,
  selected,
  onSelect,
  detail,
  detailLoading,
  onUse,
}: {
  templates: AgentTemplateSummary[];
  loading: boolean;
  search: string;
  onSearch: (value: string) => void;
  selected: AgentTemplateSummary | null;
  onSelect: (template: AgentTemplateSummary) => void;
  detail: { instructions: string } | undefined;
  detailLoading: boolean;
  onUse: () => void;
}) {
  const { t } = useT("agents");
  return (
    <main className="grid min-h-0 flex-1 grid-cols-1 lg:grid-cols-[minmax(360px,0.9fr)_minmax(0,1.1fr)]">
      <section className="flex min-h-0 flex-col border-r">
        <div className="border-b p-4">
          <div className="relative">
            <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" aria-hidden="true" />
            <Input
              name="template-search"
              autoComplete="off"
              value={search}
              onChange={(event) => onSearch(event.target.value)}
              placeholder={t(($) => $.creation_studio.templates.search)}
              className="pl-9"
              aria-label={t(($) => $.creation_studio.templates.search_label)}
            />
          </div>
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto p-3">
          {loading ? (
            <div className="flex justify-center py-16"><Loader2 className="size-5 animate-spin text-muted-foreground" /></div>
          ) : templates.length === 0 ? (
            <div className="py-16 text-center text-sm text-muted-foreground">{t(($) => $.creation_studio.templates.empty)}</div>
          ) : (
            <div className="space-y-2">
              {templates.map((template) => (
                <button
                  key={template.slug}
                  type="button"
                  onClick={() => onSelect(template)}
                  className={cn(
                    "flex w-full items-start gap-3 rounded-lg border p-3 text-left transition-colors",
                    "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                    selected?.slug === template.slug ? "border-primary bg-primary/5" : "border-transparent hover:bg-muted",
                  )}
                >
                  <span className="flex size-9 shrink-0 items-center justify-center rounded-md bg-muted"><Bot className="size-4" /></span>
                  <span className="min-w-0 flex-1">
                    <span className="flex items-center gap-2">
                      <span className="truncate text-sm font-medium">{template.name}</span>
                      {template.category && <span className="ml-auto shrink-0 text-[10px] text-muted-foreground">{template.category}</span>}
                    </span>
                    <span className="mt-1 line-clamp-2 block text-xs leading-5 text-muted-foreground">{template.description}</span>
                  </span>
                </button>
              ))}
            </div>
          )}
        </div>
      </section>
      <section className="min-h-0 overflow-y-auto">
        {!selected ? (
          <div className="flex h-full min-h-80 flex-col items-center justify-center px-8 text-center text-muted-foreground">
            <Bot className="size-8" />
            <p className="mt-3 text-sm">{t(($) => $.creation_studio.templates.select_hint)}</p>
          </div>
        ) : (
          <div className="mx-auto max-w-3xl p-6 sm:p-8">
            <div className="flex items-start gap-4">
              <span className="flex size-12 shrink-0 items-center justify-center rounded-xl bg-primary/10 text-primary"><Bot className="size-6" /></span>
              <div><h2 className="text-xl font-semibold">{selected.name}</h2><p className="mt-1 text-sm leading-6 text-muted-foreground">{selected.description}</p></div>
            </div>
            {selected.skills.length > 0 && (
              <div className="mt-7"><h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">{t(($) => $.creation_studio.templates.skills)}</h3><div className="mt-3 space-y-2">{selected.skills.map((skill) => <div key={skill.source_url} className="flex items-start gap-2 rounded-lg border bg-card p-3"><Check className="mt-0.5 size-4 shrink-0 text-success" /><div><div className="text-sm font-medium">{skill.cached_name}</div><div className="mt-0.5 text-xs text-muted-foreground">{skill.cached_description}</div></div></div>)}</div></div>
            )}
            <div className="mt-7"><h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">{t(($) => $.creation_studio.templates.instructions)}</h3><div className="mt-3 max-h-80 overflow-y-auto whitespace-pre-wrap rounded-lg border bg-muted/30 p-4 text-sm leading-6">{detailLoading ? <Loader2 className="size-4 animate-spin" /> : detail?.instructions}</div></div>
            <div className="mt-7 flex justify-end"><Button onClick={onUse} disabled={detailLoading || !detail}>{t(($) => $.creation_studio.templates.use)}<ChevronRight className="size-4" /></Button></div>
          </div>
        )}
      </section>
    </main>
  );
}

function ConfigurationPanel({
  draft,
  onChange,
  runtimes,
  runtimesLoading,
  members,
  currentUserId,
  createError,
  compact = false,
}: {
  draft: AgentDraft;
  onChange: (draft: AgentDraft) => void;
  runtimes: RuntimeDevice[];
  runtimesLoading: boolean;
  members: MemberWithUser[];
  currentUserId: string | null;
  createError: string | null;
  compact?: boolean;
}) {
  const { t } = useT("agents");
  const selectedRuntime = runtimes.find((runtime) => runtime.id === draft.runtimeId) ?? null;
  const set = <K extends keyof AgentDraft>(key: K, value: AgentDraft[K]) => onChange({ ...draft, [key]: value });
  const otherMembers = members.filter((member) => member.user_id !== currentUserId);

  return (
    <div className={cn("space-y-8", compact && "space-y-6")}>
      <SettingsSection
        title={t(($) => $.creation_studio.sections.identity)}
        description={t(($) => $.creation_studio.sections.identity_hint)}
      >
        <SettingsCard>
          <DraftFieldRow
            compact={compact}
            label={t(($) => $.create_dialog.avatar.change_aria)}
          >
            <div className={cn(!compact && "sm:flex sm:justify-end")}>
              <AvatarUploadControl
                variant="agent"
                value={draft.avatarUrl}
                name={draft.name}
                size={compact ? 52 : 56}
                onUploaded={(url) => set("avatarUrl", url)}
                onClear={() => set("avatarUrl", null)}
              />
            </div>
          </DraftFieldRow>
          <DraftFieldRow
            compact={compact}
            label={t(($) => $.create_dialog.name_label)}
            htmlFor="agent-create-name"
          >
            <Input
              id="agent-create-name"
              name="agent-name"
              autoComplete="off"
              aria-label={t(($) => $.create_dialog.name_label)}
              value={draft.name}
              onChange={(event) => set("name", event.target.value)}
              placeholder={t(($) => $.create_dialog.name_placeholder)}
            />
          </DraftFieldRow>
          <DraftFieldRow
            compact={compact}
            align="start"
            label={t(($) => $.create_dialog.description_label)}
            htmlFor="agent-create-description"
          >
            <Textarea
              id="agent-create-description"
              name="agent-description"
              autoComplete="off"
              aria-label={t(($) => $.create_dialog.description_label)}
              value={draft.description}
              onChange={(event) => set("description", event.target.value)}
              placeholder={t(($) => $.create_dialog.description_placeholder)}
              rows={compact ? 3 : 4}
              className="resize-y"
            />
          </DraftFieldRow>
        </SettingsCard>
      </SettingsSection>

      <SettingsSection
        title={t(($) => $.creation_studio.sections.behavior)}
        description={t(($) => $.creation_studio.sections.behavior_hint)}
      >
        <SettingsCard>
          <DraftFieldRow
            compact
            label={t(($) => $.create_dialog.instructions.label)}
            htmlFor="agent-create-instructions"
          >
            <Textarea
              id="agent-create-instructions"
              name="agent-instructions"
              autoComplete="off"
              aria-label={t(($) => $.create_dialog.instructions.label)}
              value={draft.instructions}
              onChange={(event) => set("instructions", event.target.value)}
              placeholder={t(($) => $.create_dialog.instructions.editor_placeholder)}
              rows={compact ? 9 : 12}
              className="min-h-44 resize-y font-mono text-[13px] leading-6"
            />
          </DraftFieldRow>
          <div className="px-4 py-4">
            <SkillMultiSelect
              selectedIds={draft.skillIds}
              onChange={(ids) => set("skillIds", ids)}
            />
          </div>
        </SettingsCard>
      </SettingsSection>

      <SettingsSection
        title={t(($) => $.creation_studio.sections.execution)}
        description={t(($) => $.creation_studio.sections.execution_hint)}
      >
        <SettingsCard>
          <div className={cn("grid gap-4 px-4 py-4", !compact && "sm:grid-cols-2")}>
            <RuntimePicker
              runtimes={runtimes}
              runtimesLoading={runtimesLoading}
              members={members}
              currentUserId={currentUserId}
              selectedRuntimeId={draft.runtimeId}
              onSelect={(id) => set("runtimeId", id)}
            />
            <ModelDropdown
              runtimeId={selectedRuntime?.id ?? null}
              runtimeOnline={selectedRuntime?.status === "online"}
              value={draft.model}
              onChange={(value) => set("model", value)}
              disabled={!selectedRuntime}
            />
          </div>
        </SettingsCard>
      </SettingsSection>

      <SettingsSection
        title={t(($) => $.creation_studio.sections.access)}
        description={t(($) => $.creation_studio.sections.access_hint)}
      >
        <SettingsCard>
          <div
            className="space-y-1 p-2"
            role="radiogroup"
            aria-label={t(($) => $.creation_studio.sections.access)}
          >
            {(["private", "workspace", "members"] as PermissionScope[]).map(
              (scope) => (
                <button
                  key={scope}
                  type="button"
                  role="radio"
                  aria-checked={draft.permissionScope === scope}
                  onClick={() => set("permissionScope", scope)}
                  className={cn(
                    "flex w-full items-start gap-3 rounded-md px-3 py-2.5 text-left transition-colors",
                    "hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                    draft.permissionScope === scope && "bg-muted",
                  )}
                >
                  <span
                    className={cn(
                      "mt-1 flex size-3.5 shrink-0 items-center justify-center rounded-full border",
                      draft.permissionScope === scope && "border-primary",
                    )}
                    aria-hidden="true"
                  >
                    {draft.permissionScope === scope ? (
                      <span className="size-1.5 rounded-full bg-primary" />
                    ) : null}
                  </span>
                  <span className="min-w-0">
                    <span className="block text-sm font-medium">
                      {t(($) => $.creation_studio.access[scope].title)}
                    </span>
                    <span className="mt-0.5 block text-xs leading-5 text-muted-foreground">
                      {t(($) => $.creation_studio.access[scope].description)}
                    </span>
                  </span>
                </button>
              ),
            )}
          </div>
          {draft.permissionScope === "members" ? (
            <div className="max-h-48 overflow-y-auto p-2">
              {otherMembers.map((member) => {
                const checked = draft.memberIds.has(member.user_id);
                return (
                  <label
                    key={member.user_id}
                    className="flex cursor-pointer items-center gap-2 rounded-md px-2 py-2 hover:bg-muted"
                  >
                    <Checkbox
                      checked={checked}
                      onCheckedChange={(value) => {
                        const next = new Set(draft.memberIds);
                        if (value === true) next.add(member.user_id);
                        else next.delete(member.user_id);
                        set("memberIds", next);
                      }}
                    />
                    <ActorAvatar
                      actorType="member"
                      actorId={member.user_id}
                      size="sm"
                    />
                    <span className="min-w-0 flex-1 truncate text-sm">
                      {member.name}
                    </span>
                  </label>
                );
              })}
              {draft.memberIds.size === 0 ? (
                <p className="px-2 py-1 text-xs text-destructive">
                  {t(($) => $.creation_studio.access.members.required)}
                </p>
              ) : null}
            </div>
          ) : null}
        </SettingsCard>
      </SettingsSection>

      {createError ? (
        <div
          role="alert"
          aria-live="polite"
          className="rounded-lg border border-destructive/30 bg-destructive/5 px-4 py-3 text-sm text-destructive"
        >
          {createError}
        </div>
      ) : null}
    </div>
  );
}

function DraftFieldRow({
  label,
  children,
  compact = false,
  align = "center",
  htmlFor,
}: {
  label: string;
  children: ReactNode;
  compact?: boolean;
  align?: "center" | "start";
  htmlFor?: string;
}) {
  return (
    <div
      className={cn(
        "gap-3 px-4 py-4",
        compact
          ? "flex flex-col"
          : "grid sm:grid-cols-[minmax(0,1fr)_minmax(280px,1.2fr)] sm:gap-8",
        !compact && (align === "center" ? "sm:items-center" : "sm:items-start"),
      )}
    >
      {htmlFor ? (
        <label htmlFor={htmlFor} className="text-sm font-medium">
          {label}
        </label>
      ) : (
        <div className="text-sm font-medium">{label}</div>
      )}
      <div className="min-w-0">{children}</div>
    </div>
  );
}

function BuilderSetup({ draft, onChange, runtimes, runtimesLoading, members, currentUserId, selectedRuntime, starting, error, onStart, onConnectRuntime }: { draft: AgentDraft; onChange: (draft: AgentDraft) => void; runtimes: RuntimeDevice[]; runtimesLoading: boolean; members: MemberWithUser[]; currentUserId: string | null; selectedRuntime: RuntimeDevice | null; starting: boolean; error: string | null; onStart: () => void; onConnectRuntime: () => void; }) {
  const { t } = useT("agents");
  const hasOnline = runtimes.some((runtime) => runtime.status === "online" && isRuntimeUsableForUser(runtime, currentUserId));
  return <main className="flex min-h-0 flex-1 items-center justify-center overflow-y-auto px-5 py-10"><div className="w-full max-w-xl rounded-xl border bg-card p-6 shadow-sm"><span className="flex size-11 items-center justify-center rounded-lg bg-primary/10 text-primary"><MessageSquare className="size-5" /></span><h2 className="mt-5 text-xl font-semibold">{t(($) => $.creation_studio.builder.setup_title)}</h2><p className="mt-2 text-sm leading-6 text-muted-foreground">{t(($) => $.creation_studio.builder.setup_description)}</p><div className="mt-6 space-y-4"><RuntimePicker runtimes={runtimes} runtimesLoading={runtimesLoading} members={members} currentUserId={currentUserId} selectedRuntimeId={draft.runtimeId} onSelect={(runtimeId) => onChange({ ...draft, runtimeId })} /><ModelDropdown runtimeId={selectedRuntime?.id ?? null} runtimeOnline={selectedRuntime?.status === "online"} value={draft.model} onChange={(model) => onChange({ ...draft, model })} disabled={!selectedRuntime} /></div>{error && <div role="alert" className="mt-4 text-sm text-destructive">{error}</div>}<div className="mt-6 flex justify-end">{hasOnline ? <Button onClick={onStart} disabled={starting || selectedRuntime?.status !== "online"}>{starting && <Loader2 className="size-4 animate-spin" />}{t(($) => $.creation_studio.builder.start)}</Button> : <Button onClick={onConnectRuntime}>{t(($) => $.creation_studio.builder.connect_runtime)}</Button>}</div></div></main>;
}

function BuilderConversation({
  sessionId,
  messages,
  loading,
  pendingTask,
  runtimeOnline,
  onSend,
  onStop,
  restoreDraftRequest,
  onRestoreDraftConsumed,
  error,
}: {
  sessionId: string;
  messages: ChatMessage[];
  loading: boolean;
  pendingTask: { task_id?: string; status?: string; created_at?: string } | undefined;
  runtimeOnline: boolean;
  onSend: (content: string) => Promise<boolean>;
  onStop: () => void;
  restoreDraftRequest: { id: string; content: string } | null;
  onRestoreDraftConsumed: () => void;
  error: string | null;
}) {
  const { t } = useT("agents");
  const pending = !!pendingTask?.task_id;
  const draftKey = `agent-builder:${sessionId}`;
  const prompts = [
    t(($) => $.creation_studio.builder.prompt_review),
    t(($) => $.creation_studio.builder.prompt_research),
    t(($) => $.creation_studio.builder.prompt_assistant),
  ];

  return (
    <section className="flex min-h-0 flex-col bg-background">
      <header className="flex min-h-14 shrink-0 items-center justify-between gap-4 border-b px-5 py-2.5">
        <div className="min-w-0">
          <h2 className="truncate text-sm font-semibold">
            {t(($) => $.creation_studio.builder.chat_title)}
          </h2>
          <p className="truncate text-xs text-muted-foreground">
            {t(($) => $.creation_studio.builder.chat_hint)}
          </p>
        </div>
        <div className="flex shrink-0 items-center gap-1.5 text-xs text-muted-foreground">
          <span
            className={cn(
              "size-2 rounded-full",
              runtimeOnline ? "bg-success" : "bg-muted-foreground/40",
            )}
            aria-hidden="true"
          />
          {runtimeOnline
            ? t(($) => $.creation_studio.builder.runtime_online)
            : t(($) => $.creation_studio.builder.runtime_offline)}
        </div>
      </header>

      {loading ? (
        <ChatMessageSkeleton />
      ) : messages.length > 0 || pending ? (
        <ChatMessageList
          messages={messages}
          pendingTask={pendingTask}
          availability={runtimeOnline ? "online" : "offline"}
          transformContent={stripBuilderDraft}
        />
      ) : (
        <div className="flex min-h-0 flex-1 items-center justify-center overflow-y-auto px-5 py-8">
          <div className="w-full max-w-xl text-center">
            <h3 className="text-balance text-lg font-semibold">
              {t(($) => $.creation_studio.builder.empty_title)}
            </h3>
            <p className="mx-auto mt-2 max-w-md text-pretty text-sm leading-6 text-muted-foreground">
              {t(($) => $.creation_studio.builder.empty_description)}
            </p>
            <div className="mt-5 flex flex-wrap justify-center gap-2">
              {prompts.map((prompt) => (
                <button
                  key={prompt}
                  type="button"
                  onClick={() => void onSend(prompt)}
                  className="rounded-full border bg-background px-3 py-1.5 text-xs transition-colors hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                >
                  {prompt}
                </button>
              ))}
            </div>
          </div>
        </div>
      )}

      {error ? (
        <div
          role="alert"
          aria-live="polite"
          className="mx-5 mb-3 rounded-md bg-destructive/5 px-3 py-2 text-sm text-destructive"
        >
          {error}
        </div>
      ) : null}

      <ChatInput
        onSend={(content) => onSend(content)}
        onStop={onStop}
        isRunning={pending}
        disabled={!runtimeOnline}
        agentName={t(($) => $.creation_studio.builder.chat_title)}
        draftKeyOverride={draftKey}
        editorKeyOverride={draftKey}
        restoreDraftRequest={restoreDraftRequest}
        onRestoreDraftConsumed={onRestoreDraftConsumed}
      />
    </section>
  );
}

function StudioFooter({ canCreate, creating, squad, onCreate }: { canCreate: boolean; creating: boolean; squad: boolean; onCreate: () => void; }) { const { t } = useT("agents"); return <div className="sticky bottom-0 mt-8 flex items-center justify-end gap-3 border-t bg-background/95 px-5 py-3 backdrop-blur"><Button type="button" onClick={onCreate} disabled={!canCreate}>{creating && <Loader2 className="size-4 animate-spin" />}{creating ? t(($) => $.creation_studio.creating) : squad ? t(($) => $.creation_studio.create_and_add) : t(($) => $.creation_studio.create_and_open)}</Button></div>; }
export function buildInvocationTargets(
  draft: AgentDraft,
): AgentInvocationTargetInput[] {
  if (draft.permissionScope === "private") return [];
  if (draft.permissionScope === "workspace") {
    return [{ target_type: "workspace" }];
  }
  return [
    ...[...draft.memberIds].map((targetId) => ({
      target_type: "member" as const,
      target_id: targetId,
    })),
    ...[...draft.teamIds].map((targetId) => ({
      target_type: "team" as const,
      target_id: targetId,
    })),
  ];
}

export function deriveDuplicateAccess(
  agent: Pick<Agent, "permission_mode" | "invocation_targets">,
): Pick<AgentDraft, "permissionScope" | "memberIds" | "teamIds"> {
  if (agent.permission_mode !== "public_to") {
    return {
      permissionScope: "private",
      memberIds: new Set(),
      teamIds: new Set(),
    };
  }

  const targets = agent.invocation_targets ?? [];
  if (targets.some((target) => target.target_type === "workspace")) {
    return {
      permissionScope: "workspace",
      memberIds: new Set(),
      teamIds: new Set(),
    };
  }

  const memberIds = targets
    .filter((target) => target.target_type === "member" && target.target_id)
    .map((target) => target.target_id as string);
  const teamIds = targets
    .filter((target) => target.target_type === "team" && target.target_id)
    .map((target) => target.target_id as string);
  if (memberIds.length === 0 && teamIds.length === 0) {
    return {
      permissionScope: "private",
      memberIds: new Set(),
      teamIds: new Set(),
    };
  }
  return {
    permissionScope: "members",
    memberIds: new Set(memberIds),
    teamIds: new Set(teamIds),
  };
}
export function parseBuilderDraft(content: string): BuilderDraftPayload | null {
  const match = content.match(/<agent_draft>([\s\S]*?)<\/agent_draft>/);
  if (!match?.[1]) return null;
  try {
    const value = JSON.parse(match[1]);
    return value && typeof value === "object"
      ? (value as BuilderDraftPayload)
      : null;
  } catch {
    // Some CLI-backed models emit literal newlines in the Markdown
    // instructions string even when asked for compact JSON. Repair only JSON
    // control characters that occur inside strings; object structure and all
    // other syntax still have to pass JSON.parse.
    try {
      const value = JSON.parse(escapeJsonStringControlCharacters(match[1]));
      return value && typeof value === "object"
        ? (value as BuilderDraftPayload)
        : null;
    } catch {
      return null;
    }
  }
}

function escapeJsonStringControlCharacters(value: string): string {
  let result = "";
  let inString = false;
  let escaped = false;

  for (const character of value) {
    if (!inString) {
      result += character;
      if (character === '"') inString = true;
      continue;
    }
    if (escaped) {
      result += character;
      escaped = false;
      continue;
    }
    if (character === "\\") {
      result += character;
      escaped = true;
      continue;
    }
    if (character === '"') {
      result += character;
      inString = false;
      continue;
    }
    if (character === "\n") {
      result += "\\n";
    } else if (character === "\r") {
      result += "\\r";
    } else if (character === "\t") {
      result += "\\t";
    } else {
      result += character;
    }
  }

  return result;
}

export function stripBuilderDraft(content: string): string {
  return content.replace(/<agent_draft>[\s\S]*?<\/agent_draft>/g, "").trim();
}

export function encodeBuilderInput(
  request: string,
  draft: AgentDraft,
  skills: Array<{ id: string; name: string; description: string }>,
  members: Array<{ user_id: string; name: string }>,
  runtime: Pick<RuntimeDevice, "id" | "name" | "provider"> | null,
  models: RuntimeModel[] | null,
): string {
  return (
    BUILDER_INPUT_PREFIX +
    JSON.stringify(
      {
        user_request: request,
        current_draft: {
          name: draft.name,
          description: draft.description,
          instructions: draft.instructions,
          model: draft.model,
          skill_ids: [...draft.skillIds],
          permission_scope: draft.permissionScope,
          member_ids: [...draft.memberIds],
        },
        selected_runtime: runtime
          ? {
              id: runtime.id,
              name: runtime.name,
              provider: runtime.provider,
            }
          : null,
        available_runtime_models:
          models === null
            ? null
            : models.map((model) => ({
                id: model.id,
                label: model.label,
                provider: model.provider,
              })),
        available_workspace_skills: skills.map((skill) => ({
          id: skill.id,
          name: skill.name,
          description: skill.description,
        })),
        available_workspace_members: members.map((member) => ({
          id: member.user_id,
          name: member.name,
        })),
      },
      null,
      2,
    )
  );
}

export function decodeBuilderInput(content: string): string {
  if (!content.startsWith(BUILDER_INPUT_PREFIX)) return content;
  try {
    const parsed = JSON.parse(content.slice(BUILDER_INPUT_PREFIX.length)) as {
      user_request?: unknown;
    };
    return typeof parsed.user_request === "string"
      ? parsed.user_request
      : content;
  } catch {
    return content;
  }
}

export function mergeBuilderDraft(
  current: AgentDraft,
  payload: BuilderDraftPayload,
  validSkillIds: Set<string>,
  validMemberIds: Set<string>,
  validModelIds: ReadonlySet<string> | null,
): AgentDraft {
  const scope =
    payload.permission_scope === "workspace" ||
    payload.permission_scope === "members" ||
    payload.permission_scope === "private"
      ? payload.permission_scope
      : current.permissionScope;
  const skillIds = Array.isArray(payload.skill_ids)
    ? payload.skill_ids.filter(
        (id): id is string =>
          typeof id === "string" && validSkillIds.has(id),
      )
    : [...current.skillIds];
  const memberIds = Array.isArray(payload.member_ids)
    ? payload.member_ids.filter(
        (id): id is string =>
          typeof id === "string" && validMemberIds.has(id),
      )
    : [...current.memberIds];
  // The current value may be a deliberate custom entry from ModelDropdown,
  // so preserving it is always safe. Only catalog IDs may be introduced by
  // the builder; failed discovery therefore cannot turn into fail-open input.
  const model =
    typeof payload.model === "string" &&
    (payload.model === current.model ||
      (validModelIds !== null &&
        validModelIds.size > 0 &&
        (payload.model === "" || validModelIds.has(payload.model))))
      ? payload.model
      : current.model;

  return {
    ...current,
    name: typeof payload.name === "string" ? payload.name : current.name,
    description:
      typeof payload.description === "string"
        ? payload.description
        : current.description,
    instructions:
      typeof payload.instructions === "string"
        ? payload.instructions
        : current.instructions,
    model,
    skillIds: new Set(skillIds),
    permissionScope: scope,
    memberIds: new Set(scope === "members" ? memberIds : []),
    teamIds: current.teamIds,
  };
}
