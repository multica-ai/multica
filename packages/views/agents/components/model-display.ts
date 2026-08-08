import type { AgentRuntime } from "@multica/core/types";
import {
  isPiRuntimeModelConfigured,
  runtimeDefaultPiConfig,
} from "@multica/core/runtimes";

export interface RuntimeDefaultModelDisplay {
  model: string;
  provider: string;
}

export function runtimeDefaultModelDisplay(
  runtime: AgentRuntime | null | undefined,
): RuntimeDefaultModelDisplay | null {
  if (!runtime || !isPiRuntimeModelConfigured(runtime)) return null;
  const config = runtimeDefaultPiConfig(runtime);
  const model = config.model?.trim();
  if (!model) return null;
  return {
    model,
    provider: config.provider?.trim() ?? "",
  };
}

export function modelDisplayLabel({
  model,
  runtime,
  fallback,
  runtimeDefaultPrefix,
}: {
  model: string | null | undefined;
  runtime: AgentRuntime | null | undefined;
  fallback: string;
  runtimeDefaultPrefix: (model: string) => string;
}): string {
  const trimmed = model?.trim();
  if (trimmed) return trimmed;
  const runtimeDefault = runtimeDefaultModelDisplay(runtime);
  if (runtimeDefault) return runtimeDefaultPrefix(runtimeDefault.model);
  return fallback;
}
