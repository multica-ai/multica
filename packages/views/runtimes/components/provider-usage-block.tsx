"use client";

import { useQuery } from "@tanstack/react-query";
import type { ProviderUsageSnapshot, ProviderUsageWindow } from "@multica/core/types";
import { runtimeProviderUsageOptions } from "@multica/core/runtimes/queries";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { useT } from "../../i18n";
import { planUsageSnapshotsForProvider } from "./runtime-plan-usage-cell";

interface ProviderUsageBlockProps {
  wsId: string;
  runtimeId: string;
  /** Protocol family of the open runtime (`claude`, `codex`, `cursor`, …). */
  provider: string | undefined;
}

export function ProviderUsageBlock({
  wsId,
  runtimeId,
  provider,
}: ProviderUsageBlockProps) {
  const { t, i18n } = useT("runtimes");
  const { data, isLoading } = useQuery(runtimeProviderUsageOptions(wsId, runtimeId));
  const providers = planUsageSnapshotsForProvider(provider, data?.providers);
  const locale = i18n.resolvedLanguage ?? i18n.language;

  return (
    <section className="flex flex-col gap-3">
      <div className="flex flex-col gap-1">
        <h3 className="text-body font-medium">{t(($) => $.usage.provider_limits.title)}</h3>
        <p className="text-caption text-muted-foreground">
          {t(($) => $.usage.provider_limits.description)}
        </p>
      </div>
      {isLoading ? (
        <Skeleton className="h-16 w-full" />
      ) : providers.length === 0 ? (
        <p className="text-caption text-muted-foreground">
          {t(($) => $.usage.provider_limits.waiting)}
        </p>
      ) : (
        <div className="flex flex-col gap-4">
          {providers.map((snapshot, index) => (
            <ProviderUsageCard
              key={`${snapshot.provider || "provider"}-${index}`}
              snapshot={snapshot}
              locale={locale}
            />
          ))}
        </div>
      )}
    </section>
  );
}

function ProviderUsageCard({
  snapshot,
  locale,
}: {
  snapshot: ProviderUsageSnapshot;
  locale: string;
}) {
  const { t } = useT("runtimes");
  const windows = snapshot.windows ?? [];
  const collected = formatWhen(snapshot.collected_at, locale);
  const plan = snapshot.plan_name?.trim() ?? "";

  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <p className="text-body font-medium">{providerLabel(snapshot.provider, t)}</p>
        {plan ? (
          <p className="text-caption text-muted-foreground">
            {t(($) => $.usage.provider_limits.plan)}: {plan}
          </p>
        ) : null}
      </div>
      {windows.length === 0 ? (
        <p className="text-caption text-muted-foreground">{reasonLabel(snapshot.reason_code, t)}</p>
      ) : (
        <div className="flex flex-col gap-2">
          {windows.map((window, index) => (
            <WindowRow key={`${window.id || "window"}-${index}`} window={window} locale={locale} />
          ))}
        </div>
      )}
      {collected ? (
        <p className="text-caption text-muted-foreground">
          {t(($) => $.usage.provider_limits.collected, { time: collected })}
        </p>
      ) : null}
    </div>
  );
}

function WindowRow({ window, locale }: { window: ProviderUsageWindow; locale: string }) {
  const { t } = useT("runtimes");
  const percent =
    typeof window.percent_used === "number" && Number.isFinite(window.percent_used)
      ? Math.round(window.percent_used)
      : null;
  const resets = formatWhen(window.resets_at, locale);

  return (
    <div className="flex flex-col gap-0.5">
      <div className="flex items-baseline justify-between gap-3">
        <span className="text-body">{windowLabel(window.id, t)}</span>
        {percent !== null ? (
          <span className="text-body font-medium">
            {t(($) => $.usage.provider_limits.used, { pct: percent })}
          </span>
        ) : null}
      </div>
      {resets ? (
        <p className="text-caption text-muted-foreground">
          {t(($) => $.usage.provider_limits.resets, { time: resets })}
        </p>
      ) : null}
    </div>
  );
}

function providerLabel(
  provider: string | undefined,
  t: ReturnType<typeof useT<"runtimes">>["t"],
): string {
  switch (provider) {
    case "claude":
      return t(($) => $.usage.provider_limits.provider_claude);
    case "cursor":
      return t(($) => $.usage.provider_limits.provider_cursor);
    case "codex":
      return t(($) => $.usage.provider_limits.provider_codex);
    case "copilot":
      return t(($) => $.usage.provider_limits.provider_copilot);
    case "antigravity":
      return t(($) => $.usage.provider_limits.provider_antigravity);
    case "grok":
      return t(($) => $.usage.provider_limits.provider_grok);
    case "kimi":
      return t(($) => $.usage.provider_limits.provider_kimi);
    case "kiro":
      return t(($) => $.usage.provider_limits.provider_kiro);
    case "opencode":
      return t(($) => $.usage.provider_limits.provider_opencode);
    default:
      return provider?.trim() || t(($) => $.usage.provider_limits.reason_unknown);
  }
}

function windowLabel(
  id: string | undefined,
  t: ReturnType<typeof useT<"runtimes">>["t"],
): string {
  switch (id) {
    case "session":
      return t(($) => $.usage.provider_limits.window_session);
    case "weekly_all":
      return t(($) => $.usage.provider_limits.window_weekly_all);
    case "auto":
      return t(($) => $.usage.provider_limits.window_auto);
    case "api":
      return t(($) => $.usage.provider_limits.window_api);
    case "primary":
      return t(($) => $.usage.provider_limits.window_primary);
    case "secondary":
      return t(($) => $.usage.provider_limits.window_secondary);
    case "premium_interactions":
      return t(($) => $.usage.provider_limits.window_premium_interactions);
    case "chat":
      return t(($) => $.usage.provider_limits.window_chat);
    case "completions":
      return t(($) => $.usage.provider_limits.window_completions);
    case "rolling":
      return t(($) => $.usage.provider_limits.window_rolling);
    case "weekly":
      return t(($) => $.usage.provider_limits.window_weekly);
    case "monthly":
      return t(($) => $.usage.provider_limits.window_monthly);
    case "credits":
      return t(($) => $.usage.provider_limits.window_credits);
    case "bonus":
      return t(($) => $.usage.provider_limits.window_bonus);
    case "gemini_hourly":
      return t(($) => $.usage.provider_limits.window_gemini_hourly);
    case "gemini_weekly":
      return t(($) => $.usage.provider_limits.window_gemini_weekly);
    case "third_party_hourly":
      return t(($) => $.usage.provider_limits.window_third_party_hourly);
    case "third_party_weekly":
      return t(($) => $.usage.provider_limits.window_third_party_weekly);
    default: {
      if (id?.startsWith("weekly_")) {
        const name = id.slice("weekly_".length).replaceAll("_", " ").trim();
        if (name) return name;
      }
      return id?.trim() || t(($) => $.usage.provider_limits.reason_unknown);
    }
  }
}

function reasonLabel(
  reason: string | undefined,
  t: ReturnType<typeof useT<"runtimes">>["t"],
): string {
  switch (reason) {
    case "not_logged_in":
      return t(($) => $.usage.provider_limits.reason_not_logged_in);
    case "api_key_only":
      return t(($) => $.usage.provider_limits.reason_api_key_only);
    case "unauthorized":
      return t(($) => $.usage.provider_limits.reason_unauthorized);
    case "cli_unavailable":
      return t(($) => $.usage.provider_limits.reason_cli_unavailable);
    case "session_unavailable":
      return t(($) => $.usage.provider_limits.reason_session_unavailable);
    case "unsupported":
      return t(($) => $.usage.provider_limits.reason_unsupported);
    default:
      return t(($) => $.usage.provider_limits.empty);
  }
}

function formatWhen(value: string | undefined, locale: string): string {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return new Intl.DateTimeFormat(locale, {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(date);
}
