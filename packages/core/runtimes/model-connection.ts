import type { AgentRuntime } from "../types";
import {
  parsePiRuntimeConfig,
  validatePiRuntimeConfig,
  type PiRuntimeConfig,
} from "../agents/pi-runtime-config";

export function runtimeDefaultPiConfig(
  runtime: Pick<AgentRuntime, "default_model_config">,
): PiRuntimeConfig {
  return parsePiRuntimeConfig(runtime.default_model_config);
}

/**
 * Undefined means the backend predates runtime model connections. Callers
 * should preserve the legacy UI instead of labelling an unknown state as
 * misconfigured.
 */
export function runtimeSupportsDefaultModelConnection(
  runtime: Pick<AgentRuntime, "has_default_model_api_key">,
): boolean {
  return runtime.has_default_model_api_key !== undefined;
}

export function isPiRuntimeModelConfigured(
  runtime: Pick<
    AgentRuntime,
    "provider" | "default_model_config" | "has_default_model_api_key"
  >,
): boolean {
  if (runtime.provider !== "pi") return true;
  if (runtime.has_default_model_api_key !== true) return false;
  const config = runtimeDefaultPiConfig(runtime);
  return validatePiRuntimeConfig(config) === null && hasAllFields(config);
}

function hasAllFields(config: PiRuntimeConfig): boolean {
  return Boolean(
    config.provider?.trim() &&
      config.api?.trim() &&
      config.baseUrl?.trim() &&
      config.model?.trim(),
  );
}
