"use client";

// FIR-2208: allowlist row button retired. Data-source access is now governed
// by the tool-policy chain (cerebro_tool_policy rows). This stub keeps the
// exports so existing imports compile while rendering nothing.

export const FIRTAL_REGISTRY_TOOL_KEY = "firtal_registry";

export function FirtalRegistryRowConfigure(_props: {
  toolKey: string;
  agentId: string;
  variant?: "ghost" | "outline" | "secondary";
  size?: "sm" | "default";
}): null {
  return null;
}
