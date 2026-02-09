/**
 * Provider Registry
 *
 * Central registry for all LLM providers with metadata,
 * status checking, and display formatting.
 */

import { credentialManager } from "../credentials.js";
import {
  hasValidClaudeCliCredentials,
  hasValidCodexCliCredentials,
} from "./oauth/cli-credentials.js";

// ============================================================
// Types
// ============================================================

export type AuthMethod = "api-key" | "oauth";

export interface ProviderInfo {
  id: string;
  name: string;
  authMethod: AuthMethod;
  available: boolean;
  configured: boolean;
  current: boolean;
  defaultModel: string;
  models: string[];
  loginUrl?: string | undefined;
  loginCommand?: string | undefined;
}

/** Static provider metadata (without runtime status) */
export interface ProviderMeta {
  id: string;
  name: string;
  authMethod: AuthMethod;
  defaultModel: string;
  models: string[];
  loginUrl?: string | undefined;
  loginCommand?: string | undefined;
}

// ============================================================
// Provider Registry
// ============================================================

const PROVIDER_REGISTRY: Record<string, ProviderMeta> = {
  "claude-code": {
    id: "claude-code",
    name: "Claude Code (OAuth)",
    authMethod: "oauth",
    defaultModel: "claude-opus-4-6",
    models: ["claude-opus-4-6", "claude-opus-4-5", "claude-sonnet-4-5", "claude-sonnet-4-0", "claude-haiku-4-5"],
    loginCommand: "claude login",
  },
  "openai-codex": {
    id: "openai-codex",
    name: "Codex (OAuth)",
    authMethod: "oauth",
    defaultModel: "gpt-5.2",
    models: ["gpt-5.2", "gpt-5.2-codex", "gpt-5.1-codex", "gpt-5.1-codex-mini", "gpt-5.1-codex-max", "gpt-5-mini"],
    loginCommand: "codex login",
  },
  "anthropic": {
    id: "anthropic",
    name: "Anthropic (API Key)",
    authMethod: "api-key",
    defaultModel: "claude-sonnet-4-5",
    models: ["claude-opus-4-6", "claude-opus-4-5", "claude-sonnet-4-5", "claude-sonnet-4-0", "claude-haiku-4-5"],
    loginUrl: "https://console.anthropic.com/",
  },
  "openai": {
    id: "openai",
    name: "OpenAI",
    authMethod: "api-key",
    defaultModel: "gpt-4o",
    models: ["gpt-5.2", "gpt-5-mini", "gpt-4.1", "gpt-4.1-mini", "gpt-4o", "gpt-4o-mini", "o3", "o3-mini"],
    loginUrl: "https://platform.openai.com/api-keys",
  },
  "kimi-coding": {
    id: "kimi-coding",
    name: "Kimi Code",
    authMethod: "api-key",
    defaultModel: "kimi-k2-thinking",
    models: ["kimi-k2-thinking", "k2p5"],
    loginUrl: "https://kimi.moonshot.cn/",
  },
  "google": {
    id: "google",
    name: "Google AI",
    authMethod: "api-key",
    defaultModel: "gemini-2.5-flash",
    models: ["gemini-2.5-pro", "gemini-2.5-flash", "gemini-2.0-flash"],
    loginUrl: "https://aistudio.google.com/apikey",
  },
  "groq": {
    id: "groq",
    name: "Groq",
    authMethod: "api-key",
    defaultModel: "llama-3.3-70b-versatile",
    models: ["llama-3.3-70b-versatile"],
    loginUrl: "https://console.groq.com/keys",
  },
  "xai": {
    id: "xai",
    name: "xAI (Grok)",
    authMethod: "api-key",
    defaultModel: "grok-4",
    models: ["grok-4", "grok-beta"],
    loginUrl: "https://console.x.ai/",
  },
  "openrouter": {
    id: "openrouter",
    name: "OpenRouter",
    authMethod: "api-key",
    defaultModel: "anthropic/claude-sonnet-4-5",
    models: ["anthropic/claude-sonnet-4-5", "anthropic/claude-opus-4-5", "openai/gpt-4o"],
    loginUrl: "https://openrouter.ai/keys",
  },
};

/**
 * Provider alias mapping for OAuth providers.
 * Maps friendly names to actual pi-ai provider names.
 */
export const PROVIDER_ALIAS: Record<string, string> = {
  "claude-code": "anthropic", // Claude Code OAuth uses anthropic API
  "openai-codex": "openai", // Codex OAuth uses OpenAI API
};

// ============================================================
// Status Checking
// ============================================================

/**
 * Check if a provider is configured with API key in credentials.json5
 */
function isApiKeyConfigured(providerId: string): boolean {
  const config = credentialManager.getLlmProviderConfig(providerId);
  return !!config?.apiKey;
}

/**
 * Check if OAuth provider has valid credentials
 */
function isOAuthAvailable(providerId: string): boolean {
  if (providerId === "claude-code") {
    return hasValidClaudeCliCredentials();
  }
  if (providerId === "openai-codex") {
    return hasValidCodexCliCredentials();
  }
  return false;
}

/**
 * Check if a provider uses OAuth authentication
 */
export function isOAuthProvider(providerId: string): boolean {
  const info = PROVIDER_REGISTRY[providerId];
  return info?.authMethod === "oauth";
}

/**
 * Check if provider is available (has valid credentials)
 */
export function isProviderAvailable(providerId: string): boolean {
  const info = PROVIDER_REGISTRY[providerId];
  if (!info) return false;

  if (info.authMethod === "oauth") {
    return isOAuthAvailable(providerId);
  }
  return isApiKeyConfigured(providerId);
}

/**
 * Get current provider from credentials
 */
export function getCurrentProvider(): string {
  return credentialManager.getLlmProvider() ?? "kimi-coding";
}

// ============================================================
// Provider Listing
// ============================================================

/**
 * Get static provider metadata
 */
export function getProviderMeta(providerId: string): ProviderMeta | undefined {
  return PROVIDER_REGISTRY[providerId];
}

/**
 * Get default model for a provider
 */
export function getDefaultModel(providerId: string): string | undefined {
  return PROVIDER_REGISTRY[providerId]?.defaultModel;
}

/**
 * Get list of all providers with their runtime status
 */
export function getProviderList(): ProviderInfo[] {
  const currentProvider = getCurrentProvider();

  return Object.values(PROVIDER_REGISTRY).map((meta) => {
    const isOAuth = meta.authMethod === "oauth";
    const available = isOAuth ? isOAuthAvailable(meta.id) : isApiKeyConfigured(meta.id);

    // Check if this is the current provider
    // For claude-code, check if current is "anthropic" and OAuth is available
    let isCurrent = currentProvider === meta.id;
    if (meta.id === "claude-code" && currentProvider === "anthropic") {
      isCurrent = hasValidClaudeCliCredentials();
    }

    return {
      ...meta,
      available,
      configured: available,
      current: isCurrent,
    };
  });
}

/**
 * Get available providers only
 */
export function getAvailableProviders(): ProviderInfo[] {
  return getProviderList().filter((p) => p.available);
}

// ============================================================
// Display Helpers
// ============================================================

/**
 * Format provider for display
 */
export function formatProviderStatus(provider: ProviderInfo): string {
  const status = provider.available ? "✓" : "✗";
  const current = provider.current ? " (current)" : "";
  const auth = provider.authMethod === "oauth" ? " [OAuth]" : "";
  return `${status} ${provider.name}${auth}${current}`;
}

/**
 * Get login instructions for a provider
 */
export function getLoginInstructions(providerId: string): string {
  const info = PROVIDER_REGISTRY[providerId];
  if (!info) return `Unknown provider: ${providerId}`;

  if (info.authMethod === "oauth") {
    if (info.loginCommand) {
      return `Run: ${info.loginCommand}\nThen retry in Super Multica to use the credentials.`;
    }
  }

  if (info.loginUrl) {
    return `Get your API key at: ${info.loginUrl}\nThen add it to ~/.super-multica/credentials.json5`;
  }

  return "No login instructions available.";
}
