// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  buildCreateAgentRequest,
  buildDuplicateDraft,
  EMPTY_AGENT_DRAFT,
  toStoredAgentDraft,
  useManualAgentDraftStore,
  type AgentDraft,
  type StoredAgentDraft,
} from "@multica/core/agents";
import { EMPTY_SKILL_SUMMARY } from "@multica/core/api/schemas";
import type { Agent, SkillSummary } from "@multica/core/types";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { useBuilderDraftSync } from "./use-builder-draft-sync";
import { useCreateAgentForm } from "./use-create-agent-form";
import { useManualDraftSync } from "./use-manual-draft-sync";

const api = vi.hoisted(() => ({
  listRuntimes: vi.fn(),
  listMembers: vi.fn(),
  listSkills: vi.fn(),
  saveAgentBuilderDraft: vi.fn(),
}));
const workspace = vi.hoisted(() => ({ id: "ws-1" }));

vi.mock("@multica/core/api", async (importOriginal) => ({
  ...await importOriginal<typeof import("@multica/core/api")>(),
  api,
}));
vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => workspace.id,
}));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: Object.assign(
    (selector: (state: { user: null }) => unknown) => selector({ user: null }),
    { getState: () => ({ user: null }) },
  ),
}));

const skill = { ...EMPTY_SKILL_SUMMARY, id: "keep", workspace_id: "ws-1" };
const deletedSkill = { ...skill, id: "deleted" };
const staleDraft = (): AgentDraft => ({
  ...EMPTY_AGENT_DRAFT,
  name: "My edited agent",
  instructions: "Keep my instructions",
  runtimeId: "runtime-1",
  skillIds: new Set([skill.id, deletedSkill.id]),
});

describe("useCreateAgentForm skill reconciliation", () => {
  let qc: QueryClient;

  function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  }

  beforeEach(() => {
    workspace.id = "ws-1";
    qc = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    });
    vi.clearAllMocks();
    api.listRuntimes.mockResolvedValue([]);
    api.listMembers.mockResolvedValue([]);
    api.listSkills.mockResolvedValue([skill]);
    api.saveAgentBuilderDraft.mockResolvedValue(undefined);
  });

  afterEach(() => {
    cleanup();
    qc.clear();
    useManualAgentDraftStore.getState().clearDraft();
  });

  it("waits for a successful catalog before pruning a draft", async () => {
    let resolveSkills!: (skills: SkillSummary[]) => void;
    api.listSkills.mockReturnValue(new Promise<SkillSummary[]>((resolve) => {
      resolveSkills = resolve;
    }));
    const { result } = renderHook(() => useCreateAgentForm(), { wrapper: Wrapper });
    const draft = staleDraft();
    act(() => result.current.setDraft(draft));
    expect(result.current.draft).toBe(draft);

    await act(async () => resolveSkills([skill]));
    await waitFor(() => expect([...result.current.draft.skillIds]).toEqual([skill.id]));
    expect(result.current.draft).toEqual({ ...draft, skillIds: new Set([skill.id]) });
    expect(result.current.draft.skillIds.size).toBe(result.current.workspaceSkills.length);
    expect(buildCreateAgentRequest({
      draft: result.current.draft,
      runtimeId: draft.runtimeId,
    }).skill_ids).toEqual([skill.id]);
  });

  it("cleans a duplicate seeded after the catalog has loaded", () => {
    qc.setQueryData(workspaceKeys.skills(workspace.id), [skill]);
    const { result } = renderHook(() => useCreateAgentForm(), { wrapper: Wrapper });
    const source: Agent = {
      id: "agent-1",
      workspace_id: "ws-1",
      runtime_id: "runtime-1",
      name: "Source",
      description: "",
      instructions: "",
      avatar_url: null,
      runtime_mode: "local",
      runtime_config: {},
      custom_args: [],
      visibility: "private",
      skills: [skill, deletedSkill],
      permission_mode: "private",
      invocation_targets: [],
      status: "idle",
      max_concurrent_tasks: 1,
      model: "",
      owner_id: "user-1",
      created_at: "",
      updated_at: "",
      archived_at: null,
      archived_by: null,
    };
    act(() => result.current.setDraft(buildDuplicateDraft(source, {
      runtimes: [],
      currentUserId: null,
      fallbackRuntimeId: "runtime-1",
      nameSuffix: " copy",
    })));

    expect([...result.current.draft.skillIds]).toEqual([skill.id]);
    expect(result.current.draft.name).toBe("Source copy");
  });

  it("persists the cleaned local draft and restores it on reopening", () => {
    qc.setQueryData(workspaceKeys.skills(workspace.id), [skill]);
    useManualAgentDraftStore.getState().setDraft({
      byOwner: { blank: { runtimeId: "runtime-1", draft: toStoredAgentDraft(staleDraft(), null) } },
    });
    const mount = () => renderHook(() => {
      const form = useCreateAgentForm();
      useManualDraftSync({ duplicateId: null, ...form, ready: true });
      return form;
    }, { wrapper: Wrapper });
    const first = mount();

    expect([...first.result.current.draft.skillIds]).toEqual([skill.id]);
    expect(useManualAgentDraftStore.getState().draft.byOwner.blank?.draft.skill_ids).toEqual([skill.id]);
    first.unmount();
    expect(mount().result.current.draft).toEqual({ ...staleDraft(), skillIds: new Set([skill.id]) });
  });

  it("cleans and saves a builder draft that arrives after the catalog", () => {
    qc.setQueryData(workspaceKeys.skills(workspace.id), [skill]);
    const initialProps: {
      stored: StoredAgentDraft | null;
      storedSettled: boolean;
    } = { stored: null, storedSettled: false };
    const { result, rerender, unmount } = renderHook((props: typeof initialProps) => {
      const form = useCreateAgentForm();
      useBuilderDraftSync({
        ...form,
        ...props,
        sessionId: "session-1",
        runtimeId: "runtime-1",
      });
      return form;
    }, { initialProps, wrapper: Wrapper });
    rerender({ stored: toStoredAgentDraft(staleDraft(), "message-1"), storedSettled: true });

    expect([...result.current.draft.skillIds]).toEqual([skill.id]);
    unmount();
    expect(api.saveAgentBuilderDraft).toHaveBeenCalledWith("session-1", expect.objectContaining({
      name: staleDraft().name,
      skill_ids: [skill.id],
      applied_message_id: "message-1",
    }));
  });

  it("follows catalog deletions while preserving edits and valid selections", async () => {
    qc.setQueryData(workspaceKeys.skills(workspace.id), [skill, deletedSkill]);
    const { result } = renderHook(() => useCreateAgentForm(), { wrapper: Wrapper });
    const draft = staleDraft();
    act(() => result.current.setDraft(draft));
    expect(result.current.draft).toBe(draft);

    act(() => qc.setQueryData(workspaceKeys.skills(workspace.id), [skill]));

    await waitFor(() => expect(result.current.draft).toEqual({ ...draft, skillIds: new Set([skill.id]) }));
  });

  it("clears all selections for a successfully loaded empty catalog", () => {
    qc.setQueryData(workspaceKeys.skills(workspace.id), []);
    const { result } = renderHook(() => useCreateAgentForm(), { wrapper: Wrapper });
    act(() => result.current.setDraft(staleDraft()));

    expect(result.current.draft.skillIds.size).toBe(0);
  });

  it("preserves selections on request failure and cleans them after recovery", async () => {
    api.listSkills.mockRejectedValue(new Error("Offline"));
    const { result } = renderHook(() => useCreateAgentForm(), { wrapper: Wrapper });
    const draft = staleDraft();
    act(() => result.current.setDraft(draft));
    await waitFor(() => expect(qc.getQueryState(workspaceKeys.skills(workspace.id))?.status).toBe("error"));
    expect(result.current.draft).toBe(draft);

    api.listSkills.mockResolvedValue([skill]);
    await act(async () => { await qc.refetchQueries({ queryKey: workspaceKeys.skills(workspace.id) }); });
    await waitFor(() => expect([...result.current.draft.skillIds]).toEqual([skill.id]));
  });

  it("does not prune a late draft against cached data after a failed refresh", async () => {
    qc.setQueryData(workspaceKeys.skills(workspace.id), [skill]);
    const { result } = renderHook(() => useCreateAgentForm(), { wrapper: Wrapper });
    api.listSkills.mockRejectedValue(new Error("Offline"));
    await act(async () => { await qc.refetchQueries({ queryKey: workspaceKeys.skills(workspace.id) }); });
    await waitFor(() => expect(qc.getQueryState(workspaceKeys.skills(workspace.id))?.status).toBe("error"));

    const draft = staleDraft();
    act(() => result.current.setDraft(draft));

    expect(result.current.draft).toBe(draft);
  });

  it("waits for the current workspace catalog after switching workspaces", async () => {
    qc.setQueryData(workspaceKeys.skills("ws-1"), []);
    let resolveSkills!: (skills: SkillSummary[]) => void;
    api.listSkills.mockReturnValue(new Promise<SkillSummary[]>((resolve) => { resolveSkills = resolve; }));
    const { result, rerender } = renderHook(() => useCreateAgentForm(), { wrapper: Wrapper });
    workspace.id = "ws-2";
    rerender();
    const draft = staleDraft();
    act(() => result.current.setDraft(draft));
    expect(result.current.draft).toBe(draft);

    await act(async () => resolveSkills([{ ...skill, workspace_id: "ws-2" }]));
    await waitFor(() => expect([...result.current.draft.skillIds]).toEqual([skill.id]));
  });
});
