import { observer } from "mobx-react-lite";
import type { JSX } from "react";
import { Button } from "../../../app/renderer/components/ui/button";
import { translate } from "../../../i18n/index";
import { useOpenAgentEnvPanel } from "../../agentEnv";
import {
  classifyRecoverableAgentMessage,
  isSharedCallerOwnerRemediableError,
  resolveAgentErrorPresentation
} from "../../agentEnv/agentErrorPresentation";
import { useAgentVisibleErrorPresentation } from "../../visibleError/AgentVisibleErrorPresentationContext";
import { workspaceAgentProviderLabel } from "../../workspaceAgentProviderLabel";
import type { AgentMessageContentVM } from "../contracts/agentMessageRowVM";
import { AgentMessageDetailsDisclosure } from "./AgentMessageDetailsDisclosure";

// All error banners use the light-red danger surface. Yellow/warning surfaces
// are banned for notice boxes — see "Badges And Status" in
// docs/conventions/desktop-visual-language.md.
const ERROR_BANNER_CLASS_NAME =
  "border-[var(--on-danger-hover)] bg-[var(--on-danger)] text-[var(--state-danger)]";

// Builds a synthetic visibleError from a plain message whose text and terminal
// status identify a recognizable environment failure.
export function recoverVisibleErrorFromMessage(
  message: AgentMessageContentVM,
  provider: string | null | undefined
): AgentMessageContentVM | null {
  const code = classifyRecoverableAgentMessage({
    body: message.body,
    statusKind: message.statusKind
  });
  if (!code) {
    return null;
  }
  return {
    ...message,
    visibleError: {
      code,
      phase: null,
      provider: provider ?? null,
      detail: message.body,
      retryable: null
    }
  };
}

export const AgentVisibleErrorMessage = observer(
  function AgentVisibleErrorMessage({
    message,
    onExternalLink
  }: {
    message: AgentMessageContentVM;
    onAuthLogin?: (provider?: string | null) => void;
    onExternalLink?: (href: string) => void;
  }): JSX.Element {
    "use memo";
    const openAgentEnvPanel = useOpenAgentEnvPanel();
    const { overrides: presentationOverrides, scope } =
      useAgentVisibleErrorPresentation();
    const error = message.visibleError;

    // One card for every run-failure code. The presentation (keyed on the codes
    // the daemon actually emits — see agentErrorPresentation) supplies a granular,
    // provider-aware message and, when the failure is something the env wizard can
    // detect or repair, a single deep-linking call-to-action. Transient/server-side
    // failures resolve to no focus, so no (misleading) wizard button is shown.
    const providerLabel = workspaceAgentProviderLabel(
      error?.provider ?? "unknown"
    );
    const presentation = resolveAgentErrorPresentation(error?.code, scope);
    const insufficientCreditsOverride =
      presentationOverrides?.insufficient_credits;
    const presentationOverride =
      scope !== "shared_caller" &&
      error?.code === "insufficient_credits" &&
      insufficientCreditsOverride?.providers.includes(error.provider ?? "")
        ? insufficientCreditsOverride
        : undefined;
    const rawDetail = error?.detail ?? "";
    const providerHeadline =
      error?.origin === "provider" && rawDetail.trim() ? rawDetail : null;
    const headline =
      providerHeadline ||
      presentationOverride?.message ||
      (presentation?.messageKey
        ? translate(presentation.messageKey, { provider: providerLabel })
        : visibleErrorTitle(message));
    const focus = presentation?.focus ?? null;
    const actionKey = presentation?.actionKey ?? null;
    const externalAction = presentationOverride?.action ?? null;
    const hint = visibleErrorHint(message, scope);
    const showRawDetail =
      error?.origin !== "provider" &&
      error?.detailAvailable === true &&
      rawDetail !== "";
    // Account limits are status notices, not process crashes. Provider payloads
    // stay in the canonical model and diagnostics; raw upstream text is only
    // rendered through the explicit disclosure below.
    const isPlanOrQuotaLimit =
      error?.code === "quota_or_rate_limit" ||
      error?.code === "insufficient_credits" ||
      error?.code === "model_not_allowed" ||
      error?.code === "subscription_required";
    return (
      <section
        role={isPlanOrQuotaLimit ? "status" : "alert"}
        className={`box-border w-full min-w-0 rounded-[8px] border p-3 text-[13px] leading-5 text-[var(--text-primary)] ${ERROR_BANNER_CLASS_NAME}`}
      >
        <div className="flex min-w-0 items-center gap-3">
          <div className="min-w-0 flex-1">
            <div className="font-medium text-[var(--text-primary)]">
              {headline}
            </div>
            {scope === "shared_caller" && error?.code ? (
              <code className="mt-1 block break-all font-mono text-[10px] leading-4 text-[var(--text-secondary)]">
                {error.code}
              </code>
            ) : null}
            {hint ? (
              <div className="mt-1 text-[11px] text-[var(--text-secondary)]">
                {hint}
              </div>
            ) : null}
            {showRawDetail ? (
              <AgentMessageDetailsDisclosure
                detail={rawDetail}
                className="mt-1"
                label={translate("agentHost.agentGui.visibleErrorRawDetails")}
              />
            ) : null}
          </div>
          {(externalAction?.url && onExternalLink) ||
          (actionKey && focus && openAgentEnvPanel) ? (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="shrink-0"
              onClick={() => {
                if (externalAction?.url) {
                  onExternalLink?.(externalAction.url);
                  return;
                }
                if (focus && openAgentEnvPanel) {
                  openAgentEnvPanel({
                    provider: error?.provider ?? null,
                    focus
                  });
                }
              }}
            >
              {externalAction?.label ||
                (actionKey ? translate(actionKey) : null)}
            </Button>
          ) : null}
        </div>
      </section>
    );
  }
);

function visibleErrorTitle(message: AgentMessageContentVM): string {
  const error = message.visibleError;
  const provider = workspaceAgentProviderLabel(error?.provider ?? "unknown");
  switch (error?.code) {
    case "auth_required":
      return translate("agentHost.agentGui.visibleErrorAuthRequired", {
        provider
      });
    case "request_timed_out":
      return translate("agentHost.agentGui.visibleErrorRequestTimedOut", {
        provider
      });
    case "runtime_unavailable":
      return translate("agentHost.agentGui.visibleErrorRuntimeUnavailable", {
        provider
      });
    case "quota_or_rate_limit":
      return translate("agentHost.agentGui.visibleErrorQuotaOrRateLimit", {
        provider
      });
    default:
      if (error?.phase === "start") {
        return translate("agentHost.agentGui.visibleErrorStartFailed", {
          provider
        });
      }
      return translate("agentHost.agentGui.visibleErrorRequestFailed", {
        provider
      });
  }
}

function visibleErrorHint(
  message: AgentMessageContentVM,
  scope: "local_owner" | "shared_caller"
): string | null {
  const error = message.visibleError;
  if (
    scope === "shared_caller" &&
    isSharedCallerOwnerRemediableError(error?.code)
  ) {
    return translate("agentHost.agentGui.visibleErrorSharedCallerHint");
  }
  if (error?.code !== "auth_required") {
    return null;
  }
  return translate(
    "agentHost.agentGui.visibleErrorAuthRequiredLocalAgentHint",
    {
      provider: workspaceAgentProviderLabel(error.provider ?? "unknown")
    }
  );
}
