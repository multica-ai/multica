// Data for ProviderPickerDemo, kept free of React so the guard test can load it.
//
// Capabilities as the code declares them: resume — the backend resumes a stored
// session (server/pkg/agent/<provider>.go); mcp — providerSupportsMcpConfig
// (packages/core/agents/mcp-support.ts); model — ModelSelectionSupported
// (server/pkg/agent/models.go). Keep in step with the "Supported tools"
// table on the providers page; provider-picker-tools.test.ts checks every locale.
export const TOOLS: { name: string; resume: boolean; mcp: boolean; model: boolean }[] = [
  { name: "Antigravity", resume: true, mcp: false, model: true },
  { name: "Claude Code", resume: true, mcp: true, model: true },
  { name: "CodeBuddy", resume: true, mcp: true, model: true },
  { name: "CodeArts", resume: true, mcp: true, model: true },
  { name: "Codex", resume: true, mcp: true, model: true },
  { name: "GitHub Copilot CLI", resume: true, mcp: false, model: true },
  { name: "Cursor Agent", resume: true, mcp: true, model: true },
  { name: "DeepSeek Harness", resume: true, mcp: true, model: true },
  { name: "DevEco Code", resume: true, mcp: false, model: true },
  { name: "Grok", resume: true, mcp: true, model: true },
  { name: "Hermes", resume: true, mcp: true, model: true },
  { name: "Kimi CLI", resume: true, mcp: true, model: true },
  { name: "Kiro CLI", resume: true, mcp: true, model: true },
  { name: "MiniMax Code", resume: false, mcp: true, model: false },
  { name: "Muse Code", resume: true, mcp: false, model: true },
  { name: "Devin CLI", resume: true, mcp: true, model: true },
  { name: "OpenClaw", resume: true, mcp: true, model: true },
  { name: "OpenCode", resume: true, mcp: true, model: true },
  { name: "Pi", resume: true, mcp: true, model: true },
  { name: "Oh-My-Pi", resume: true, mcp: true, model: true },
  { name: "Qoder CLI", resume: true, mcp: true, model: true },
  { name: "Qoder CN CLI", resume: true, mcp: true, model: true },
  { name: "Qwen Code", resume: true, mcp: true, model: true },
  { name: "QwenPaw", resume: true, mcp: true, model: false },
  { name: "Amp", resume: true, mcp: false, model: true },
  { name: "Factory Droid", resume: true, mcp: false, model: true },
  { name: "Reasonix", resume: true, mcp: true, model: true },
  { name: "Trae CLI", resume: true, mcp: true, model: true },
  { name: "Dim", resume: true, mcp: true, model: true },
  { name: "ZeroClaw", resume: true, mcp: false, model: false },
  { name: "ZCode", resume: true, mcp: true, model: true },
];
