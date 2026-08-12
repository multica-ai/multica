import {
  isPiRuntimeModelConfigured,
  runtimeDefaultPiConfig,
} from "@multica/core/runtimes";
import type { AgentRuntime, RuntimeModel } from "@multica/core/types";
import { piProviderCatalogModels } from "../../runtimes/model-presets";

const PI_MODEL_DISCOVERY_NOISE = new Set(["no/models"]);

type RuntimeModelSource = Pick<
  AgentRuntime,
  "provider" | "default_model_config" | "has_default_model_api_key"
>;

export function isPiModelDiscoveryNoise(value: string): boolean {
  return PI_MODEL_DISCOVERY_NOISE.has(value.trim().toLowerCase());
}

export function visibleRuntimeModels(
  models: RuntimeModel[],
  runtime: RuntimeModelSource | null | undefined,
): RuntimeModel[] {
  if (runtime?.provider !== "pi") return models;
  const discovered = models.filter(
    (model) =>
      !isPiModelDiscoveryNoise(model.id) &&
      !isPiModelDiscoveryNoise(model.label),
  );
  return mergeRuntimeModels(discovered, piRuntimeConnectionModels(runtime));
}

function piRuntimeConnectionModels(
  runtime: RuntimeModelSource,
): RuntimeModel[] {
  if (!isPiRuntimeModelConfigured(runtime)) return [];
  const config = runtimeDefaultPiConfig(runtime);
  const provider = config.provider?.trim();
  const defaultModel = config.model?.trim();
  if (!provider || !defaultModel) return [];

  const modelIds = new Set<string>(piProviderCatalogModels(provider));
  modelIds.add(defaultModel);

  return Array.from(modelIds).map((id) => ({
    id,
    label: id,
    provider,
    default: id === defaultModel,
  }));
}

function mergeRuntimeModels(
  discovered: RuntimeModel[],
  configured: RuntimeModel[],
): RuntimeModel[] {
  if (configured.length === 0) return discovered;
  const out = [...discovered];
  const seen = new Set<string>();
  for (const model of discovered) {
    for (const key of modelIdentityKeys(model)) {
      seen.add(key);
    }
  }
  for (const model of configured) {
    if (modelIdentityKeys(model).some((key) => seen.has(key))) {
      continue;
    }
    out.push(model);
    for (const key of modelIdentityKeys(model)) {
      seen.add(key);
    }
  }
  return out;
}

function modelIdentityKeys(model: RuntimeModel): string[] {
  const keys = new Set<string>();
  const provider = model.provider?.trim();
  addModelIdentity(keys, model.id, provider);
  addModelIdentity(keys, model.label, provider);
  return Array.from(keys);
}

function addModelIdentity(
  keys: Set<string>,
  raw: string | undefined,
  provider: string | undefined,
) {
  const value = raw?.trim();
  if (!value) return;
  keys.add(value.toLowerCase());
  if (provider) {
    keys.add(`${provider}/${value}`.toLowerCase());
    const prefix = `${provider}/`.toLowerCase();
    if (value.toLowerCase().startsWith(prefix)) {
      keys.add(value.slice(prefix.length).toLowerCase());
    }
  }
}
