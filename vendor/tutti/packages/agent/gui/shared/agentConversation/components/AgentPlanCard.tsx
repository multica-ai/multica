import { Check, ChevronDown, ChevronUp, Copy, ListChecks } from "lucide-react";
import { observer } from "mobx-react-lite";
import { useCallback, useRef, useState, type JSX, type ReactNode } from "react";
import { translate } from "../../../i18n/index";

const COLLAPSED_MAX_HEIGHT_PX = 220;

/**
 * Shared collapsible plan card used by both providers' plan proposals so the
 * codex plan item and the claude-code ExitPlanMode plan render identically:
 * a framed card with a title, a copy action, and a collapse/expand affordance
 * (long plans default to collapsed with an "expand plan" overlay).
 */
export const AgentPlanCard = observer(function AgentPlanCard({
  title,
  copyText,
  children,
  defaultCollapsed = true
}: {
  title?: string;
  copyText: string;
  children: ReactNode;
  defaultCollapsed?: boolean;
}): JSX.Element {
  "use memo";
  const [collapsed, setCollapsed] = useState(defaultCollapsed);
  const [copied, setCopied] = useState(false);
  const copyResetRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const handleCopy = useCallback(() => {
    void navigator.clipboard?.writeText(copyText).then(() => {
      setCopied(true);
      if (copyResetRef.current) {
        clearTimeout(copyResetRef.current);
      }
      copyResetRef.current = setTimeout(() => setCopied(false), 1500);
    });
  }, [copyText]);

  return (
    <section
      data-testid="agent-plan-card"
      data-collapsed={collapsed ? "true" : "false"}
      className="box-border w-full min-w-0 rounded-[8px] border border-[var(--tutti-purple-border)] bg-[var(--tutti-purple-bg)] p-3"
    >
      <div className="mb-2 flex items-center gap-1.5">
        <ListChecks
          size={14}
          strokeWidth={2}
          aria-hidden="true"
          className="shrink-0 text-[var(--text-secondary)]"
        />
        <span
          data-testid="agent-plan-card-title"
          className="flex-1 truncate text-[12px] font-medium text-[var(--text-secondary)]"
        >
          {title ?? translate("agentHost.agentGui.planCardTitle")}
        </span>
        <button
          type="button"
          data-testid="agent-plan-card-copy"
          className="inline-flex size-6 shrink-0 items-center justify-center rounded-[6px] text-[var(--text-tertiary)] transition-colors hover:bg-[var(--transparency-hover)] hover:text-[var(--text-secondary)]"
          aria-label={translate("agentHost.agentGui.planCardCopy")}
          title={translate("agentHost.agentGui.planCardCopy")}
          onClick={handleCopy}
        >
          {copied ? (
            <Check size={14} strokeWidth={2} aria-hidden="true" />
          ) : (
            <Copy size={14} strokeWidth={2} aria-hidden="true" />
          )}
        </button>
        <button
          type="button"
          data-testid="agent-plan-card-toggle"
          className="inline-flex size-6 shrink-0 items-center justify-center rounded-[6px] text-[var(--text-tertiary)] transition-colors hover:bg-[var(--transparency-hover)] hover:text-[var(--text-secondary)]"
          aria-expanded={!collapsed}
          aria-label={translate(
            collapsed
              ? "agentHost.agentGui.planCardExpand"
              : "agentHost.agentGui.planCardCollapse"
          )}
          title={translate(
            collapsed
              ? "agentHost.agentGui.planCardExpand"
              : "agentHost.agentGui.planCardCollapse"
          )}
          onClick={() => setCollapsed((value) => !value)}
        >
          {collapsed ? (
            <ChevronDown size={15} strokeWidth={2} aria-hidden="true" />
          ) : (
            <ChevronUp size={15} strokeWidth={2} aria-hidden="true" />
          )}
        </button>
      </div>
      <div className="relative">
        <div
          data-testid="agent-plan-card-body"
          className="min-w-0 overflow-hidden"
          style={
            collapsed
              ? { maxHeight: `${COLLAPSED_MAX_HEIGHT_PX}px` }
              : undefined
          }
        >
          {children}
        </div>
        {collapsed ? (
          <div className="pointer-events-none absolute inset-x-0 bottom-0 flex justify-center bg-gradient-to-t from-[var(--tutti-purple-bg)] via-[color-mix(in_srgb,var(--tutti-purple-bg)_70%,transparent)] to-transparent pt-8 pb-1">
            <button
              type="button"
              data-testid="agent-plan-card-expand"
              className="pointer-events-auto inline-flex h-7 items-center rounded-full border border-[var(--line-2)] bg-[var(--background-fronted)] px-3 text-[12px] font-medium text-[var(--text-secondary)] transition-colors hover:text-[var(--text-primary)]"
              onClick={() => setCollapsed(false)}
            >
              {translate("agentHost.agentGui.planCardExpand")}
            </button>
          </div>
        ) : null}
      </div>
    </section>
  );
});
