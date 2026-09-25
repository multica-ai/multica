import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import type {
  IssueStatus,
  IssuePriority,
  IssueAssigneeType,
  IssuePropertyValues,
} from "../../types";
import type { CreateMode } from "./create-mode-store";
import type { QuickCreateActorType } from "./quick-create-store";
import { createWorkspaceAwareStorage, registerForWorkspaceRehydration } from "../../platform/workspace-storage";
import { defaultStorage } from "../../platform/storage";
import { registerDraftCleanup } from "../../drafts/cleanup-registry";
import { normalizeStoredUploads, type DraftUpload } from "../../drafts/draft-upload";

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
  /** Uploads for the dialog (placeholders + completed), referenced by the
   *  manual description OR the agent prompt markdown. A single pool so an
   *  image survives a mode switch from either side; each submit path sends
   *  only the ids its own content references. Coordinator-owned (MUL-5181 L2):
   *  a placeholder written at pick time survives dialog close, and one still
   *  `uploading` at load time is dropped on rehydrate. */
  attachments: DraftUpload[];
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
  /** In-memory only. While present, writes target an isolated source-context
   *  create session and persistence continues to serialize this ordinary
   *  backup, so a reload cannot leak the source draft into normal create. */
  isolatedDraftBackup?: IssueCreateDraft;
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
  beginIsolatedDraft: () => void;
  endIsolatedDraft: () => void;
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
        // Legacy builds persisted bare Attachment rows; normalize wraps them
        // as `uploaded` placeholders (and drops stale `uploading` ones).
        attachments: normalizeStoredUploads(d.attachments),
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

  const sharedRaw = (d.shared as Partial<IssueCreateShared> & { attachments?: unknown }) ?? {};
  return {
    shared: {
      ...emptyShared(),
      ...sharedRaw,
      // Pre-L2 nested drafts stored bare Attachment rows; every load also
      // drops `uploading` placeholders (bytes are gone).
      attachments: normalizeStoredUploads(sharedRaw.attachments),
    },
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
      beginIsolatedDraft: () =>
        set((s) => {
          if (s.isolatedDraftBackup) return s;
          return {
            isolatedDraftBackup: s.draft,
            draft: {
              shared: emptyShared(),
              manual: {
                ...emptyManual(),
                assigneeType: s.lastAssigneeType,
                assigneeId: s.lastAssigneeId,
              },
              agent: emptyAgent(),
              activeMode: "agent",
            },
          };
        }),
      endIsolatedDraft: () =>
        set((s) => s.isolatedDraftBackup
          ? { draft: s.isolatedDraftBackup, isolatedDraftBackup: undefined }
          : s),
      setLastAssignee: (type, id) =>
        set({ lastAssigneeType: type, lastAssigneeId: id }),
      hasDraft: () => {
        const { manual, agent, shared } = get().draft;
        return !!(
          manual.title ||
          manual.description ||
          agent.prompt ||
          Object.keys(manual.propertyValues).length > 0 ||
          // Recoverable uploads only: a failed/interrupted remnant the user
          // never dismissed must not pin the sidebar's draft dot forever.
          shared.attachments.some((u) => u.status === "uploaded" || u.status === "uploading")
        );
      },
    }),
    {
      name: "multica_issue_draft",
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
      // An isolated source-context draft must never reach localStorage. Persist
      // the ordinary backup throughout that session; a crash/reload therefore
      // restores the user's normal create draft, not source-specific input.
      partialize: (state) => ({
        draft: state.isolatedDraftBackup ?? state.draft,
        lastAssigneeType: state.lastAssigneeType,
        lastAssigneeId: state.lastAssigneeId,
      }),
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
  // Full reset, NOT clearDraft(): clearDraft deliberately keeps the
  // last-assignee preference and re-seeds it into the fresh draft's manual
  // slot — correct between drafts of one user, but on logout it would hand
  // the previous user's last-picked assignee to the next login on this tab.
  resetInMemory: () =>
    useIssueDraftStore.setState({
      draft: migrateDraft(undefined),
      lastAssigneeType: undefined,
      lastAssigneeId: undefined,
      isolatedDraftBackup: undefined,
    }),
});
