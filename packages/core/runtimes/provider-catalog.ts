export type RuntimeProviderSetup = "subscription" | "api_key" | "local";

export type RuntimeProviderExecution = "cli" | "openai-compatible";

export type RuntimeProviderCapability =
  | "streaming"
  | "model-selection"
  | "mcp";

export interface RuntimeProviderDescriptor {
  id: string;
  displayName: string;
  setup: RuntimeProviderSetup;
  execution: RuntimeProviderExecution;
  defaultBaseUrl?: string;
  baseUrlEnv?: string;
  apiKeyEnv?: string;
  optionalKeyEnv?: readonly string[];
  capabilities: readonly RuntimeProviderCapability[];
}

// This catalog is intentionally limited to the replacement runtimes that are
// supported by the daemon's native CLI backends or shared API adapter. It is
// metadata only: the browser never reads, stores, or submits credential values.
export const REPLACEMENT_RUNTIME_PROVIDERS = [
  {
    id: "codex",
    displayName: "ChatGPT subscription",
    setup: "subscription",
    execution: "cli",
    capabilities: ["streaming", "model-selection", "mcp"],
  },
  {
    id: "claude",
    displayName: "Claude subscription",
    setup: "subscription",
    execution: "cli",
    capabilities: ["streaming", "model-selection", "mcp"],
  },
  {
    id: "antigravity",
    displayName: "Google Antigravity subscription",
    setup: "subscription",
    execution: "cli",
    capabilities: ["streaming", "model-selection"],
  },
  {
    id: "cursor",
    displayName: "Cursor subscription",
    setup: "subscription",
    execution: "cli",
    capabilities: ["streaming", "model-selection", "mcp"],
  },
  {
    id: "grok",
    displayName: "Grok subscription",
    setup: "subscription",
    execution: "cli",
    capabilities: ["streaming", "model-selection", "mcp"],
  },
  {
    id: "opencode-api",
    displayName: "OpenCode Console API",
    setup: "api_key",
    execution: "openai-compatible",
    defaultBaseUrl: "https://opencode.ai/inference/openai/v1",
    baseUrlEnv: "OPENCODE_API_BASE_URL",
    apiKeyEnv: "OPENCODE_API_KEY",
    optionalKeyEnv: ["OPENCODE_API_TOKEN"],
    capabilities: ["streaming", "model-selection", "mcp"],
  },
  {
    id: "opencode-zen",
    displayName: "OpenCode Zen",
    setup: "api_key",
    execution: "openai-compatible",
    defaultBaseUrl: "https://opencode.ai/zen/v1",
    baseUrlEnv: "OPENCODE_ZEN_BASE_URL",
    apiKeyEnv: "OPENCODE_ZEN_API_KEY",
    optionalKeyEnv: ["OPENCODE_ZEN_TOKEN"],
    capabilities: ["streaming", "model-selection", "mcp"],
  },
  {
    id: "opencode-go",
    displayName: "OpenCode Go",
    setup: "api_key",
    execution: "openai-compatible",
    defaultBaseUrl: "https://opencode.ai/zen/go/v1",
    baseUrlEnv: "OPENCODE_GO_BASE_URL",
    apiKeyEnv: "OPENCODE_GO_API_KEY",
    optionalKeyEnv: ["OPENCODE_GO_TOKEN"],
    capabilities: ["streaming", "model-selection", "mcp"],
  },
  {
    id: "openrouter",
    displayName: "OpenRouter API",
    setup: "api_key",
    execution: "openai-compatible",
    defaultBaseUrl: "https://openrouter.ai/api/v1",
    baseUrlEnv: "OPENROUTER_BASE_URL",
    apiKeyEnv: "OPENROUTER_API_KEY",
    capabilities: ["streaming", "model-selection", "mcp"],
  },
  {
    id: "vercel-ai-gateway",
    displayName: "Vercel AI Gateway API",
    setup: "api_key",
    execution: "openai-compatible",
    defaultBaseUrl: "https://ai-gateway.vercel.sh/v1",
    baseUrlEnv: "AI_GATEWAY_BASE_URL",
    apiKeyEnv: "AI_GATEWAY_API_KEY",
    optionalKeyEnv: ["VERCEL_OIDC_TOKEN"],
    capabilities: ["streaming", "model-selection", "mcp"],
  },
  {
    id: "ollama",
    displayName: "Ollama",
    setup: "local",
    execution: "openai-compatible",
    defaultBaseUrl: "http://127.0.0.1:11434/v1",
    baseUrlEnv: "OLLAMA_BASE_URL",
    apiKeyEnv: "OLLAMA_API_KEY",
    capabilities: ["streaming", "model-selection", "mcp"],
  },
  {
    id: "lmstudio",
    displayName: "LM Studio",
    setup: "local",
    execution: "openai-compatible",
    defaultBaseUrl: "http://127.0.0.1:1234/v1",
    baseUrlEnv: "LMSTUDIO_BASE_URL",
    apiKeyEnv: "LMSTUDIO_API_KEY",
    capabilities: ["streaming", "model-selection", "mcp"],
  },
  {
    id: "nvidia-nim",
    displayName: "NVIDIA NIM API",
    setup: "api_key",
    execution: "openai-compatible",
    defaultBaseUrl: "https://integrate.api.nvidia.com/v1",
    baseUrlEnv: "NVIDIA_NIM_BASE_URL",
    apiKeyEnv: "NVIDIA_API_KEY",
    capabilities: ["streaming", "model-selection", "mcp"],
  },
] as const satisfies readonly RuntimeProviderDescriptor[];

export type ReplacementRuntimeProviderID =
  (typeof REPLACEMENT_RUNTIME_PROVIDERS)[number]["id"];

export function replacementRuntimeProvider(
  id: string,
): RuntimeProviderDescriptor | undefined {
  return REPLACEMENT_RUNTIME_PROVIDERS.find((provider) => provider.id === id);
}
