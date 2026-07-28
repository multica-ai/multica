/**
 * Cloud state for the redesigned entry points (MUL-5385).
 *
 * This module deliberately contains NO capability wiring. It holds the
 * client-side state and catalogs needed to review the hierarchical
 * device → runtime → model flow before any server / Fleet work starts.
 *
 * Everything here is a mock:
 *   - `cloudOn` flips locally; nothing is provisioned, nothing is charged.
 *   - the credit / workload numbers are illustrative placeholders.
 * This branch is for the test environment only — it is not meant to merge to
 * main as-is.
 */
import { createStore } from "zustand/vanilla";
import { useStore } from "zustand";
import type {
  Agent,
  AgentInvocationTargetInput,
  AgentRuntimeMode,
  AgentSkillSummary,
  RuntimeDevice,
  RuntimeModel,
} from "@multica/core/types";

export const CLOUD_PREVIEW_RUNTIME_IDS = {
  codex: "mock-multica-cloud-codex",
  claude: "mock-multica-cloud-claude",
  gemini: "mock-multica-cloud-gemini",
} as const;

export const CLOUD_PREVIEW_RUNTIME_ID = CLOUD_PREVIEW_RUNTIME_IDS.codex;

const THINKING_LEVELS = [
  {
    value: "low",
    label: "Low",
    description: "Faster responses for straightforward tasks",
  },
  {
    value: "medium",
    label: "Medium",
    description: "Balanced reasoning for everyday work",
  },
  {
    value: "high",
    label: "High",
    description: "Deeper reasoning for complex tasks",
  },
  {
    value: "xhigh",
    label: "Extra high",
    description: "Maximum reasoning for difficult problems",
  },
];

const CLOUD_PREVIEW_MODELS_BY_RUNTIME: Record<string, RuntimeModel[]> = {
  [CLOUD_PREVIEW_RUNTIME_IDS.codex]: [
    {
      id: "gpt-5.6-sol",
      label: "GPT-5.6 Sol",
      provider: "OpenAI",
      default: true,
      thinking: { supported_levels: THINKING_LEVELS },
      service_tiers: [
        {
          id: "priority",
          name: "Fast",
        },
      ],
    },
    {
      id: "gpt-5.6-terra",
      label: "GPT-5.6 Terra",
      provider: "OpenAI",
      thinking: {
        supported_levels: THINKING_LEVELS.slice(0, 3),
      },
    },
  ],
  [CLOUD_PREVIEW_RUNTIME_IDS.claude]: [
    {
      id: "claude-sonnet-4-6",
      label: "Claude Sonnet 4.6",
      provider: "Anthropic",
      default: true,
      thinking: {
        supported_levels: THINKING_LEVELS.slice(0, 3),
      },
    },
    {
      id: "claude-opus-4-6",
      label: "Claude Opus 4.6",
      provider: "Anthropic",
      thinking: { supported_levels: THINKING_LEVELS },
    },
  ],
  [CLOUD_PREVIEW_RUNTIME_IDS.gemini]: [
    {
      id: "gemini-3.1-pro",
      label: "Gemini 3.1 Pro",
      provider: "Google",
      default: true,
    },
    {
      id: "gemini-3-flash",
      label: "Gemini 3 Flash",
      provider: "Google",
    },
  ],
};

export const CLOUD_PREVIEW_MODELS =
  CLOUD_PREVIEW_MODELS_BY_RUNTIME[CLOUD_PREVIEW_RUNTIME_ID] ?? [];

export function getCloudPreviewModels(
  runtimeId: string | null | undefined,
): RuntimeModel[] {
  if (!runtimeId) return [];
  return CLOUD_PREVIEW_MODELS_BY_RUNTIME[runtimeId] ?? [];
}

export interface CloudPreviewState {
  /** Whether the workspace has "turned on" Multica Cloud (mock). */
  cloudOn: boolean;
  /** Illustrative wallet + usage figures (mock). */
  creditsRemaining: number;
  creditsUsedThisMonth: number;
  /** Illustrative workload figures (mock). */
  activeRuns: number;
  queuedRuns: number;
  createdAgents: Agent[];
  setCloudOn: (on: boolean) => void;
  addCreatedAgent: (agent: Agent) => void;
  reset: () => void;
}

const MOCK_DEFAULTS = {
  cloudOn: false,
  creditsRemaining: 4820,
  creditsUsedThisMonth: 1180,
  activeRuns: 2,
  queuedRuns: 0,
  createdAgents: [] as Agent[],
};

export const cloudPreviewStore = createStore<CloudPreviewState>((set) => ({
  ...MOCK_DEFAULTS,
  setCloudOn: (cloudOn) => set({ cloudOn }),
  addCreatedAgent: (agent) =>
    set((state) => ({
      cloudOn: true,
      createdAgents: [
        ...state.createdAgents.filter((current) => current.id !== agent.id),
        agent,
      ],
    })),
  reset: () => set({ ...MOCK_DEFAULTS }),
}));

export function useCloudPreview<T>(selector: (state: CloudPreviewState) => T): T {
  return useStore(cloudPreviewStore, selector);
}

/** Test helper — restores mock defaults between cases. */
export function resetCloudPreview(): void {
  cloudPreviewStore.getState().reset();
}

/**
 * Keeps the preview capability enabled after a Cloud-backed create. The
 * runtime picker normally enables it before selection; this is a safeguard
 * for create flows entered through an existing Cloud runtime.
 */
export function enableCloudPreviewAfterCreate(
  runtimeMode: AgentRuntimeMode,
): void {
  if (runtimeMode === "cloud") {
    cloudPreviewStore.getState().setCloudOn(true);
  }
}

export function isCloudPreviewRuntimeId(
  runtimeId: string | null | undefined,
): boolean {
  return Object.values(CLOUD_PREVIEW_RUNTIME_IDS).some(
    (candidate) => candidate === runtimeId,
  );
}

const CLOUD_PREVIEW_RUNTIME_DEFINITIONS = [
  {
    id: CLOUD_PREVIEW_RUNTIME_IDS.codex,
    name: "Codex",
    provider: "codex",
  },
  {
    id: CLOUD_PREVIEW_RUNTIME_IDS.claude,
    name: "Claude Code",
    provider: "claude",
  },
  {
    id: CLOUD_PREVIEW_RUNTIME_IDS.gemini,
    name: "Gemini CLI",
    provider: "gemini",
  },
] as const;

export function createCloudPreviewRuntime(
  workspaceId: string,
  runtimeId: string = CLOUD_PREVIEW_RUNTIME_ID,
): RuntimeDevice {
  const timestamp = "2026-07-28T00:00:00.000Z";
  const definition =
    CLOUD_PREVIEW_RUNTIME_DEFINITIONS.find(
      (candidate) => candidate.id === runtimeId,
    ) ?? CLOUD_PREVIEW_RUNTIME_DEFINITIONS[0];
  return {
    id: definition.id,
    workspace_id: workspaceId,
    daemon_id: "multica-cloud",
    name: definition.name,
    runtime_mode: "cloud",
    provider: definition.provider,
    launch_header: "",
    status: "online",
    device_info: "Managed by Multica",
    metadata: {
      managed: true,
      elastic: true,
    },
    owner_id: null,
    visibility: "public",
    last_seen_at: timestamp,
    created_at: timestamp,
    updated_at: timestamp,
  };
}

export function createCloudPreviewRuntimes(
  workspaceId: string,
): RuntimeDevice[] {
  return CLOUD_PREVIEW_RUNTIME_DEFINITIONS.map((definition) =>
    createCloudPreviewRuntime(workspaceId, definition.id),
  );
}

export function withCloudPreviewRuntime(
  runtimes: RuntimeDevice[],
  workspaceId: string,
  currentUserId: string | null,
): RuntimeDevice[] {
  const hasUsableCloud = runtimes.some(
    (runtime) =>
      runtime.runtime_mode === "cloud" &&
      (!currentUserId ||
        runtime.owner_id === currentUserId ||
        runtime.visibility === "public"),
  );
  return hasUsableCloud
    ? runtimes
    : [...runtimes, ...createCloudPreviewRuntimes(workspaceId)];
}

export function createCloudPreviewAgent({
  workspaceId,
  runtimeId,
  ownerId,
  name,
  description,
  instructions,
  avatarUrl,
  model,
  thinkingLevel,
  serviceTier,
  permissionMode,
  invocationTargets,
  skills,
  customArgs = [],
  maxConcurrentTasks = 1,
}: {
  workspaceId: string;
  runtimeId: string;
  ownerId: string | null;
  name: string;
  description: string;
  instructions: string;
  avatarUrl: string | null;
  model: string;
  thinkingLevel?: string;
  serviceTier?: string;
  permissionMode: "private" | "public_to";
  invocationTargets: AgentInvocationTargetInput[];
  skills: AgentSkillSummary[];
  customArgs?: string[];
  maxConcurrentTasks?: number;
}): Agent {
  const now = new Date().toISOString();
  const workspaceVisible =
    permissionMode === "public_to" &&
    invocationTargets.some((target) => target.target_type === "workspace");
  const randomId =
    globalThis.crypto?.randomUUID?.() ??
    `${Date.now()}-${Math.random().toString(36).slice(2)}`;

  return {
    id: `mock-cloud-agent-${randomId}`,
    workspace_id: workspaceId,
    runtime_id: runtimeId,
    name,
    description,
    instructions,
    avatar_url: avatarUrl,
    runtime_mode: "cloud",
    runtime_config: { managed: true },
    custom_args: customArgs,
    visibility: workspaceVisible ? "workspace" : "private",
    permission_mode: permissionMode,
    invocation_targets: invocationTargets.map((target) => ({
      target_type: target.target_type,
      target_id: target.target_id ?? null,
    })),
    status: "idle",
    max_concurrent_tasks: maxConcurrentTasks,
    model,
    thinking_level: thinkingLevel ?? "",
    service_tier: serviceTier ?? "",
    owner_id: ownerId,
    skills,
    created_at: now,
    updated_at: now,
    archived_at: null,
    archived_by: null,
  };
}

export function registerCloudPreviewAgent(agent: Agent): void {
  cloudPreviewStore.getState().addCreatedAgent(agent);
}
