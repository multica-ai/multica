"use client";

// FIR-2208: allowlist dialog retired. Data-source scoping is now handled
// entirely by the tool-policy chain (cerebro_tool_policy rows). This stub keeps
// the export so existing imports compile while rendering nothing.

export function FirtalRegistryConfigDialog(_props: {
  agentId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}): null {
  return null;
}
