import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import type {
  IssueStatus,
  IssuePriority,
  IssueAssigneeType,
  IssuePropertyValues,
  Attachment,
} from "../../types";
import type { CreateMode } from "./create-mode-store";
import type { QuickCreateActorType } from "./quick-create-store";
import { createWorkspaceAwareStorage, registerForWorkspaceRehydration } from "../../platform/workspace-storage";
import { defaultStorage } from "../../platform/storage";
import { registerDraftCleanup } from "../../drafts/cleanup-registry";

// One logical Issue-Create draft (MUL-5181), split so switching between the
// manual form and the agent form never destroys the other side's content.
//
//   shared  — belongs to the issue no matter how it is filed: project,
//             priority, due date, attachments.
//   manual  — the manual form's own state: title, description, status, start
//             date, assignee, labels, custom properties.
//   agent   — the agent form's own state: the free-text prompt and the picked
//             actor (agent or squad).
//   activeMode — which form the draft is currently being edited in.
//
// Before this split, `switchToAgent` concatenated title + description into the
// prompt and then CLEARED the manual fields, and `switchToManual` copied the
// prompt into the description but left the old prompt behind — so a round-trip
// both lost manual content and resurfaced a stale prompt. With separate slots
// a switch is a no-op on the other side's data; the only cross-write is a
// one-time assist-init the panels perform when the target slot is still empty.

export interface IssueCreateShared {
  projectId?: string;
  priority: IssuePriority;
  dueDate: string | null;
  /** Uploaded attachments, referenced by the manual description OR the agent
   *  prompt markdown. A single pool so an image survives a mode switch from
   *  either side; each submit path sends only the ids its own content
   *  references. */
  attachments: Attachment[];
}

export interface IssueCreateManual {
  title: string;
  description: string;
  status: IssueStatus;
  startDate: string | null;
  assigneeType?: IssueAssigneeType;
  assigneeId?: string;
  /** Label IDs chosen in the create dialog. Attached to the issue right after
   *  it is created (the create endpoint takes no labels), so they are kept as
   *  a plain id list rather than full Label objects. */
  labelIds: string[];
  propertyValues: IssuePropertyValues;
}

export interface IssueCreateAgent {
  prompt: string;
  actorType?: QuickCreateActorType;
  actorId?: string;
}

export interface IssueCreateDraft {
  shared: IssueCreateShared;
  manual: IssueCreateManual;
  agent: IssueCreateAgent;
  activeMode: CreateMode;
}

const emptyShared = (): IssueCreateShared => ({
  projectId: undefined,
  priority: "none",
  dueDate: null,
  attachments: [],
});

const emptyManual = (): IssueCreateManual => ({
  title: "",
  description: "",
  status: "todo",
  startDate: null,
  assigneeType: undefined,
  assigneeId: undefined,
  labelIds: [],
  propertyValues: {},
});

const emptyAgent = (): IssueCreateAgent => ({
  prompt: "",
  actorType: undefined,
  actorId: undefined,
});

interface IssueDraftStore {
  draft: IssueCreateDraft;
  // Last assignee picked at submit time. Persisted across drafts so the
  // create-issue modal can prefill the picker with the user's most recent
  // choice instead of always opening with no assignee.
  lastAssigneeType?: IssueAssigneeType;
  lastAssigneeId?: string;
  setShared: (patch: Partial<IssueCreateShared>) => void;
  setManual: (patch: Partial<IssueCreateManual>) => void;
  setAgent: (patch: Partial<IssueCreateAgent>) => void;
  setActiveMode: (mode: CreateMode) => void;
  clearDraft: () => void;
  setLastAssignee: (type?: IssueAssigneeType, id?: string) => void;
  hasDraft: () => boolean;
}

function isLegacyFlatDraft(d: Record<string, unknown>): boolean {
  return (
    !("manual" in d) &&
    !("shared" in d) &&
    ("title" in d || "status" in d || "labelIds" in d || "description" in d)
  );
}

// Drafts persisted by older builds either predate a later-added sub-field or
// use the pre-MUL-5181 flat shape. Backfill defaults so every read site can
// rely on the declared IssueCreateDraft shape instead of re-defending, and lift
// a legacy flat draft into the manual/shared slots (there was no agent prompt
// in that store — it lived in `multica_quick_create` and is not carried over).
function migrateDraft(raw: unknown): IssueCreateDraft {
  const d = (raw && typeof raw === "object" ? raw : {}) as Record<string, unknown>;

  if (isLegacyFlatDraft(d)) {
    return {
      shared: {
        ...emptyShared(),
        projectId: d.projectId as string | undefined,
        priority: (d.priority as IssuePriority) ?? "none",
        dueDate: (d.dueDate as string | null) ?? null,
        attachments: Array.isArray(d.attachments) ? (d.attachments as Attachment[]) : [],
      },
      manual: {
        ...emptyManual(),
        title: (d.title as string) ?? "",
        description: (d.description as string) ?? "",
        status: (d.status as IssueStatus) ?? "todo",
        startDate: (d.startDate as string | null) ?? null,
        assigneeType: d.assigneeType as IssueAssigneeType | undefined,
        assigneeId: d.assigneeId as string | undefined,
        labelIds: Array.isArray(d.labelIds) ? (d.labelIds as string[]) : [],
        propertyValues:
          d.propertyValues && typeof d.propertyValues === "object"
            ? (d.propertyValues as IssuePropertyValues)
            : {},
      },
      agent: emptyAgent(),
      activeMode: "manual",
    };
  }

  return {
    shared: { ...emptyShared(), ...((d.shared as Partial<IssueCreateShared>) ?? {}) },
    manual: { ...emptyManual(), ...((d.manual as Partial<IssueCreateManual>) ?? {}) },
    agent: { ...emptyAgent(), ...((d.agent as Partial<IssueCreateAgent>) ?? {}) },
    activeMode: d.activeMode === "agent" ? "agent" : "manual",
  };
}

export const useIssueDraftStore = create<IssueDraftStore>()(
  persist(
    (set, get) => ({
      draft: migrateDraft(undefined),
      lastAssigneeType: undefined,
      lastAssigneeId: undefined,
      setShared: (patch) =>
        set((s) => ({ draft: { ...s.draft, shared: { ...s.draft.shared, ...patch } } })),
      setManual: (patch) =>
        set((s) => ({ draft: { ...s.draft, manual: { ...s.draft.manual, ...patch } } })),
      setAgent: (patch) =>
        set((s) => ({ draft: { ...s.draft, agent: { ...s.draft.agent, ...patch } } })),
      setActiveMode: (mode) =>
        set((s) => ({ draft: { ...s.draft, activeMode: mode } })),
      clearDraft: () =>
        set((s) => ({
          draft: {
            shared: emptyShared(),
            manual: {
              ...emptyManual(),
              assigneeType: s.lastAssigneeType,
              assigneeId: s.lastAssigneeId,
            },
            agent: emptyAgent(),
            activeMode: s.draft.activeMode,
          },
        })),
      setLastAssignee: (type, id) =>
        set({ lastAssigneeType: type, lastAssigneeId: id }),
      hasDraft: () => {
        const { manual, agent, shared } = get().draft;
        return !!(
          manual.title ||
          manual.description ||
          agent.prompt ||
          Object.keys(manual.propertyValues).length > 0 ||
          shared.attachments.length > 0
        );
      },
    }),
    {
      name: "multica_issue_draft",
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
      merge: (persistedState, currentState) => {
        const persisted = (persistedState ?? {}) as Partial<IssueDraftStore> & {
          draft?: unknown;
        };
        return {
          ...currentState,
          ...persisted,
          draft: migrateDraft(persisted.draft),
        };
      },
    },
  ),
);

registerForWorkspaceRehydration(() => useIssueDraftStore.persist.rehydrate());

registerDraftCleanup({
  storageKey: "multica_issue_draft",
  workspaceScoped: true,
  resetInMemory: () => useIssueDraftStore.getState().clearDraft(),
});
