import { MessageSquareText } from "lucide-react";
import { observer } from "mobx-react-lite";
import type { JSX } from "react";
import { useTranslation } from "../../../i18n/index";
import type { AgentSelectedTextVM } from "../contracts/agentMessageRowVM";

export const AgentSelectedTextChip = observer(function AgentSelectedTextChip({
  selectedText
}: {
  selectedText: AgentSelectedTextVM;
}): JSX.Element {
  "use memo";
  const { t } = useTranslation();
  const count = selectedText.count;
  const label =
    count === 1
      ? t("agentHost.agentGui.selectionReferenceCountOne")
      : t("agentHost.agentGui.selectionReferenceCountMany", {
          count: String(count)
        });

  return (
    <div
      data-testid="agent-selected-text-chip"
      data-selected-text-count={count}
      className="group inline-flex max-w-full items-center rounded-[10px] border border-[var(--line-1)] bg-[var(--background-fronted)] text-sm font-medium text-[var(--text-primary)]"
    >
      <span className="inline-flex min-w-0 items-center gap-2 rounded-[9px] py-2 pl-3 pr-2">
        <MessageSquareText
          aria-hidden="true"
          className="shrink-0 text-[var(--text-secondary)]"
          size={16}
          strokeWidth={2}
        />
        <span className="truncate">{label}</span>
      </span>
    </div>
  );
});
