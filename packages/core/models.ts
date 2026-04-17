export interface ModelOption {
  id: string;
  label: string;
}

export const PROVIDER_MODELS: Record<string, ModelOption[]> = {
  claude: [
    { id: "claude-opus-4-6", label: "Claude Opus 4.6" },
    { id: "claude-sonnet-4-6", label: "Claude Sonnet 4.6" },
    { id: "claude-sonnet-4-5", label: "Claude Sonnet 4.5" },
    { id: "claude-haiku-4-5", label: "Claude Haiku 4.5" },
  ],
  codex: [
    { id: "o3", label: "o3" },
    { id: "o4-mini", label: "o4-mini" },
    { id: "gpt-4.1", label: "GPT-4.1" },
  ],
  gemini: [
    { id: "gemini-2.5-pro", label: "Gemini 2.5 Pro" },
    { id: "gemini-2.5-flash", label: "Gemini 2.5 Flash" },
  ],
  opencode: [],
  openclaw: [],
  hermes: [],
};
