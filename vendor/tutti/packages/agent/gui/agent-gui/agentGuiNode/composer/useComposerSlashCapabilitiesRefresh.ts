import type { AgentComposerRenderProps as AgentComposerProps } from "./AgentComposer.types";

export function refreshComposerSlashCapabilities(input: {
  agentSessionId: string | null;
  isPaletteOpen: boolean;
  onRetryComposerOptions: AgentComposerProps["onRetryComposerOptions"];
  refreshedSessionRef: { current: string | null };
  slashQuery: string | null;
}): void {
  const refreshKey =
    input.isPaletteOpen && input.slashQuery !== null
      ? input.agentSessionId
      : null;
  if (refreshKey === null) {
    input.refreshedSessionRef.current = null;
    return;
  }
  if (input.refreshedSessionRef.current === refreshKey) {
    return;
  }
  input.refreshedSessionRef.current = refreshKey;
  input.onRetryComposerOptions?.({
    force: true,
    section: "capabilities"
  });
}
