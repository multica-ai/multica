import { useMemo } from "react";
import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import { ChevronRight } from "lucide-react";
import { observer } from "mobx-react-lite";
import { useState, type JSX } from "react";
import { translate } from "../../../i18n/index";
import { CollapsibleReveal } from "./CollapsibleReveal";

export const AgentMessageDetailsDisclosure = observer(
  function AgentMessageDetailsDisclosure({
    detail,
    className = "",
    label,
    tone = "danger"
  }: {
    detail: string;
    className?: string;
    label?: string;
    tone?: "danger" | "muted";
  }): JSX.Element {
    "use memo";
    const [expanded, setExpanded] = useState(false);
    const isMuted = tone === "muted";
    const onClickProp1 = usePersistCallback<
      NonNullable<React.ComponentProps<"button">["onClick"]>
    >(() => setExpanded((value) => !value));
    const styleProp2Value1 = expanded ? "rotate(90deg)" : "rotate(0deg)";
    const styleProp2 = useMemo<
      NonNullable<React.ComponentProps<typeof ChevronRight>["style"]>
    >(
      () => ({
        transform: styleProp2Value1,
        transformOrigin: "center",
        transition: "transform 200ms cubic-bezier(0.22, 1.18, 0.36, 1)",
        willChange: "transform"
      }),
      [styleProp2Value1]
    );
    return (
      <div
        className={`${className} text-[11px] ${isMuted ? "text-[var(--text-secondary)]" : "text-[var(--state-danger)]"}`}
      >
        <button
          type="button"
          className={`inline-flex w-fit max-w-full min-w-0 cursor-pointer select-none items-center gap-1.5 border-0 bg-transparent p-0 text-left font-[inherit] text-[inherit] transition-colors duration-150 ${isMuted ? "hover:text-[var(--text-primary)]" : "hover:text-[var(--state-danger-hover)]"}`}
          aria-expanded={expanded}
          onClick={onClickProp1}
        >
          {label ?? translate("agentHost.agentGui.visibleErrorDetails")}
          <ChevronRight
            size={12}
            strokeWidth={2.2}
            aria-hidden="true"
            className={`shrink-0 ${isMuted ? "text-[var(--text-secondary)]" : "text-[var(--state-danger)]"}`}
            style={styleProp2}
          />
        </button>
        <CollapsibleReveal expanded={expanded} preMountOnIdle>
          <pre
            className={`mt-2 max-h-[220px] overflow-auto whitespace-pre-wrap break-words rounded-[6px] px-3 py-2 font-[var(--tsh-font-mono)] text-[11px] leading-5 ${isMuted ? "bg-[var(--transparency-block)] text-[var(--text-secondary)]" : "bg-[var(--on-danger)] text-[var(--state-danger)]"}`}
          >
            {detail}
          </pre>
        </CollapsibleReveal>
      </div>
    );
  }
);
