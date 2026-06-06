import { useQuery } from "@tanstack/react-query";
import { RefreshCw, ExternalLink } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import type { AgentRuntime } from "@multica/core/types/agent";
import {
  runtimeQuotaOptions,
  useInitiateQuotaCheck,
} from "@multica/core/runtimes/queries";

const PROVIDER_DASHBOARDS: Record<string, string> = {
  cursor: "https://cursor.com/settings",
  antigravity: "https://aistudio.google.com/",
};

export function QuotaSection({ runtime }: { runtime: AgentRuntime }) {
  const { data: quota, isLoading } = useQuery({
    ...runtimeQuotaOptions(runtime.id),
    refetchInterval: (q) => {
      const status = q.state.data?.status;
      if (status === "pending" || status === "running") return 2_000;
      return false;
    },
  });
  const initiate = useInitiateQuotaCheck(runtime.id);

  const dashboardUrl = PROVIDER_DASHBOARDS[runtime.provider];
  const fetchedAt = quota?.fetched_at
    ? new Date(quota.fetched_at).toLocaleTimeString()
    : null;

  return (
    <div className="rounded-lg border bg-card text-card-foreground">
      {/* Header */}
      <div className="flex items-center justify-between px-5 py-4 border-b">
        <div className="flex items-center gap-2">
          <span className="text-sm font-medium">Rate Limits</span>
          {quota?.stale && (
            <span className="text-[11px] text-warning bg-warning/10 rounded px-1.5 py-0.5">
              устарело
            </span>
          )}
        </div>
        <div className="flex items-center gap-2">
          {fetchedAt && (
            <span className="text-xs text-muted-foreground">
              обновлено {fetchedAt}
            </span>
          )}
          <Button
            variant="ghost"
            size="icon"
            className="h-7 w-7"
            disabled={
              initiate.isPending ||
              isLoading ||
              quota?.status === "pending" ||
              quota?.status === "running"
            }
            onClick={() => initiate.mutate()}
          >
            <RefreshCw
              className={`h-3.5 w-3.5 ${initiate.isPending || quota?.status === "pending" || quota?.status === "running" ? "animate-spin" : ""}`}
            />
          </Button>
        </div>
      </div>

      {/* Body */}
      <div className="p-5">
        {/* Not supported (Cursor, Antigravity, etc.) */}
        {quota?.error === "not_supported" ? (
          <div className="flex items-center justify-between text-sm text-muted-foreground">
            <span>API лимиты недоступны для {runtime.provider}</span>
            {dashboardUrl && (
              <a
                href={dashboardUrl}
                target="_blank"
                rel="noopener noreferrer"
                className="flex items-center gap-1 text-xs text-brand hover:underline"
              >
                Дашборд провайдера
                <ExternalLink className="h-3 w-3" />
              </a>
            )}
          </div>
        ) : quota?.error && !quota.rate_requests && !quota.rate_tokens && !quota.credits_limit ? (
          /* Other error (key not set, network, etc.) */
          <div className="text-sm text-destructive">
            {quota.error === "ANTHROPIC_API_KEY not set" ||
            quota.error === "OPENAI_API_KEY not set" ||
            quota.error === "FACTORY_API_KEY not set" ? (
              <span>
                Env var{" "}
                <code className="text-xs bg-muted px-1 rounded">
                  {quota.error.replace(" not set", "")}
                </code>{" "}
                не задана в окружении daemon&apos;а
              </span>
            ) : (
              <span>{quota.error}</span>
            )}
          </div>
        ) : (quota?.status === "pending" || quota?.status === "running") && !quota.rate_requests && !quota.rate_tokens ? (
          /* Loading state */
          <div className="text-sm text-muted-foreground animate-pulse">
            Проверяю лимиты…
          </div>
        ) : !quota || quota.status === "no_data" ? (
          /* No data yet */
          <div className="text-sm text-muted-foreground">
            Нажмите обновить чтобы проверить лимиты
          </div>
        ) : (
          /* Data rows */
          <div className="space-y-4">
            {/* Droid credits limit */}
            {runtime.provider === "droid" && quota.credits_limit != null && (
              <div className="flex items-center justify-between">
                <span className="text-sm text-muted-foreground">
                  Лимит кредитов{quota.provider_note ? ` (${quota.provider_note})` : ""}
                </span>
                <div className="flex items-center gap-2">
                  <span className="text-sm font-medium tabular-nums">
                    {quota.credits_limit.toLocaleString()} / пользователь
                  </span>
                  <a
                    href="https://app.factory.ai/settings/usage"
                    target="_blank"
                    rel="noopener noreferrer"
                    className="flex items-center gap-1 text-xs text-brand hover:underline"
                  >
                    Расход
                    <ExternalLink className="h-3 w-3" />
                  </a>
                </div>
              </div>
            )}

            {/* Rate limits */}
            {quota.rate_requests && (
              <QuotaRow
                label="Запросов / мин"
                window={quota.rate_requests}
              />
            )}
            {quota.rate_tokens && (
              <QuotaRow
                label="Токенов / мин"
                window={quota.rate_tokens}
              />
            )}

            {/* Partial error note */}
            {quota.error && (quota.rate_requests || quota.rate_tokens) && (
              <p className="text-xs text-muted-foreground">{quota.error}</p>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

function QuotaRow({
  label,
  window: w,
}: {
  label: string;
  window: { limit: number; remaining: number; resets_at?: string };
}) {
  const pct = w.limit > 0 ? w.remaining / w.limit : 0;
  const barColor =
    pct > 0.5
      ? "bg-success"
      : pct > 0.2
        ? "bg-warning"
        : "bg-destructive";

  const resetsIn = w.resets_at
    ? Math.max(0, Math.round((new Date(w.resets_at).getTime() - Date.now()) / 1000))
    : null;

  return (
    <div className="space-y-1.5">
      <div className="flex items-center justify-between text-sm">
        <span className="text-muted-foreground">{label}</span>
        <span className="tabular-nums font-medium">
          {w.remaining.toLocaleString()}
          <span className="text-muted-foreground font-normal">
            {" "}/ {w.limit.toLocaleString()}
          </span>
          {resetsIn != null && (
            <span className="text-muted-foreground font-normal text-xs ml-2">
              · сброс через {resetsIn}с
            </span>
          )}
        </span>
      </div>
      <div className="h-1.5 rounded-full bg-muted overflow-hidden">
        <div
          className={`h-full rounded-full transition-all ${barColor}`}
          style={{ width: `${Math.max(2, pct * 100)}%` }}
        />
      </div>
    </div>
  );
}
