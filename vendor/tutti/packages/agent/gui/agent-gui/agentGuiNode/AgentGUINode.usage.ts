import { toLocalShortDateTime } from "../../app/renderer/shell/utils/format";
import type { TranslateFn } from "../../i18n/index";
import type { AgentUsageQuota } from "../../shared/contracts/dto";
import type { AgentGUIAgentTarget, AgentGUIProvider } from "../../types";
import type { AgentComposerSlashStatusLimit } from "./AgentComposer";
import type { useAgentGUIPage } from "./controller/useAgentGUIPage";

function slashStatusQuotaLabel(quota: AgentUsageQuota, t: TranslateFn): string {
  const modelName = quota.modelName?.trim();
  if (modelName) {
    return modelName;
  }
  switch (quota.quotaType) {
    case "session":
      return t("agentHost.agentGui.slashStatusFiveHourLimit");
    case "weekly":
      return t("agentHost.agentGui.slashStatusWeeklyLimit");
    case "daily":
      return t("agentHost.workspaceAgentProbeQuotaDaily");
    case "monthly":
      return t("agentHost.workspaceAgentProbeQuotaMonthly");
    case "cost":
      return t("agentHost.workspaceAgentProbeQuotaCost");
    case "credits":
      return t("agentHost.workspaceAgentProbeQuotaCredits");
    case "model":
      return t("agentHost.workspaceAgentProbeAgentUsage");
    default:
      return quota.quotaType;
  }
}

function slashStatusQuotaValue(quota: AgentUsageQuota, t: TranslateFn): string {
  if (
    quota.amountUnit === "credits" &&
    typeof quota.amountRemaining === "number" &&
    Number.isFinite(quota.amountRemaining)
  ) {
    return t("agentHost.workspaceAgentProbeQuotaCreditsRemaining", {
      amount: Math.max(0, quota.amountRemaining).toLocaleString("en-US", {
        maximumFractionDigits: 2
      })
    });
  }
  if (
    typeof quota.percentRemaining === "number" &&
    Number.isFinite(quota.percentRemaining)
  ) {
    return t("agentHost.agentGui.slashStatusLimitPercentLeft", {
      percent: Math.round(quota.percentRemaining)
    });
  }
  if (
    typeof quota.dollarRemaining === "number" &&
    Number.isFinite(quota.dollarRemaining)
  ) {
    return t("agentHost.workspaceAgentProbeQuotaDollarRemaining", {
      amount: quota.dollarRemaining.toFixed(2)
    });
  }
  return "";
}

function slashStatusQuotaReset(quota: AgentUsageQuota, t: TranslateFn): string {
  const reset =
    typeof quota.resetsAtUnixMs === "number" &&
    Number.isFinite(quota.resetsAtUnixMs)
      ? toLocalShortDateTime(quota.resetsAtUnixMs)
      : quota.resetText?.trim();
  return reset ? t("agentHost.agentGui.slashStatusLimitReset", { reset }) : "";
}

export function slashStatusLimitsFromQuotas(
  quotas: readonly AgentUsageQuota[] | undefined,
  selectedModel: string | null | undefined,
  t: TranslateFn
): AgentComposerSlashStatusLimit[] {
  const filteredQuotas = filterSlashStatusQuotasForModel(quotas, selectedModel);
  return filteredQuotas
    .map((quota, index): AgentComposerSlashStatusLimit | null => {
      const value = slashStatusQuotaValue(quota, t);
      if (!value) {
        return null;
      }
      const label = slashStatusQuotaLabel(quota, t).trim();
      if (!label) {
        return null;
      }
      return {
        id: `${quota.quotaType}:${quota.modelName ?? ""}:${index}`,
        label,
        percentRemaining:
          typeof quota.percentRemaining === "number" &&
          Number.isFinite(quota.percentRemaining)
            ? Math.max(0, Math.min(100, Math.round(quota.percentRemaining)))
            : null,
        value,
        reset: slashStatusQuotaReset(quota, t) || null
      };
    })
    .filter((limit): limit is AgentComposerSlashStatusLimit => limit !== null);
}

export function slashStatusUsageErrorMessage(
  code: string | null | undefined,
  t: TranslateFn
): string | null {
  if (!code) {
    return null;
  }
  switch (code) {
    case "auth_required":
      return t("agentHost.agentGui.slashStatusUsageAuthRequired");
    case "session_expired":
      return t("agentHost.agentGui.slashStatusUsageSessionExpired");
    case "subscription_required":
      return t("agentHost.agentGui.slashStatusUsageSubscriptionRequired");
    case "quota_exhausted":
      return t("agentHost.agentGui.slashStatusUsageQuotaExhausted");
    case "parse_failed":
      return t("agentHost.agentGui.slashStatusUsageParseFailed");
    default:
      return t("agentHost.agentGui.slashStatusUsageError");
  }
}

function filterSlashStatusQuotasForModel(
  quotas: readonly AgentUsageQuota[] | undefined,
  selectedModel: string | null | undefined
): readonly AgentUsageQuota[] {
  const normalizedSelectedModel = normalizeSlashStatusModelName(selectedModel);
  const baseQuotas = (quotas ?? []).filter(
    (quota) => quota.quotaType !== "model"
  );
  const matchingModelQuotas = (quotas ?? []).filter((quota) => {
    const quotaModelName = normalizeSlashStatusModelName(quota.modelName);
    return (
      quota.quotaType === "model" &&
      quotaModelName !== "" &&
      normalizedSelectedModel !== "" &&
      quotaModelName === normalizedSelectedModel
    );
  });
  return [...baseQuotas, ...matchingModelQuotas];
}

function normalizeSlashStatusModelName(
  value: string | null | undefined
): string {
  return (
    value
      ?.trim()
      .toLowerCase()
      .replace(/[^a-z0-9]+/gu, "-")
      .replace(/^-+|-+$/gu, "") ?? ""
  );
}

export function resolveAgentGUIRailStatusTarget(input: {
  conversationFilter: Pick<
    ReturnType<typeof useAgentGUIPage>,
    | "shell"
    | "rail"
    | "detail"
    | "composer"
    | "interaction"
    | "readiness"
    | "operations"
  >["rail"]["conversationFilter"];
  agentTargets: readonly AgentGUIAgentTarget[];
}): AgentGUIAgentTarget | null {
  const filter = input.conversationFilter;
  if (filter.kind !== "agentTarget") {
    return null;
  }
  return resolveAgentGUIStatusTarget({
    agentTargets: input.agentTargets,
    scopeKey: filter.agentTargetId
  });
}

export function resolveAgentGUIStatusTarget(input: {
  agentTargets: readonly AgentGUIAgentTarget[];
  scopeKey: string;
}): AgentGUIAgentTarget | null {
  const scopeKey = input.scopeKey.trim();
  if (!scopeKey) {
    return null;
  }
  return (
    input.agentTargets.find(
      (candidate) =>
        candidate.disabled !== true &&
        ((candidate.agentTargetId?.trim() ?? "") === scopeKey ||
          candidate.targetId.trim() === scopeKey)
    ) ?? null
  );
}

export function resolveAgentGUIRailConfigProvider(
  railConfigProvider: AgentGUIProvider | null | undefined,
  fallbackProvider: AgentGUIProvider
): AgentGUIProvider | null {
  return railConfigProvider === undefined
    ? fallbackProvider
    : railConfigProvider;
}
