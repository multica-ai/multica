import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import { Button } from "@multica/ui/components/ui/button";
import { TextQuote, X } from "lucide-react";
import { observer } from "mobx-react-lite";
import { useId, useLayoutEffect, useRef, useState } from "react";
import { translate } from "../../../i18n/index";
import type { AgentComposerQuoteBlock } from "../model/agentGuiNodeTypes";

export const ComposerQuoteCard = observer(function ComposerQuoteCard({
  quote,
  onRemove
}: {
  quote: AgentComposerQuoteBlock;
  onRemove: () => void;
}) {
  const [expanded, setExpanded] = useState(false);
  const [overflows, setOverflows] = useState(false);
  const text = useRef<HTMLDivElement>(null);
  const textId = useId();
  useLayoutEffect(() => {
    const element = text.current;
    if (!element || expanded) return;
    const measure = () => setOverflows(element.scrollHeight > element.clientHeight);
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    return () => observer.disconnect();
  }, [expanded, quote.text]);
  const toggle = usePersistCallback(() => setExpanded((value) => !value));
  const label = translate("agentHost.agentGui.selectionQuote");
  return (
    <div className="agent-gui-composer-quote" data-testid="agent-gui-composer-quote-drafts" role="group" aria-label={label}>
      <div className="agent-gui-composer-quote-main">
        <div className="agent-gui-composer-quote-head">
          <TextQuote size={16} strokeWidth={1.75} aria-hidden />
          <span>{label}{quote.sourceName ? ` · ${quote.sourceName}` : ""}</span>
        </div>
        <div ref={text} id={textId} className="agent-gui-composer-quote-text" data-expanded={expanded} data-testid="agent-gui-composer-quote-text">{quote.text}</div>
        {overflows && (
          <Button type="button" variant="ghost" size="xs" className="agent-gui-composer-quote-toggle" onClick={toggle} aria-expanded={expanded} aria-controls={textId}>
            {translate(expanded ? "agentHost.agentGui.selectionQuoteCollapse" : "agentHost.agentGui.selectionQuoteExpand")}
          </Button>
        )}
      </div>
      <Button type="button" variant="ghost" size="icon" className="agent-gui-composer-quote-remove" onClick={onRemove} aria-label={translate("agentHost.agentGui.selectionQuoteRemove")}>
        <X size={16} strokeWidth={1.75} aria-hidden />
      </Button>
    </div>
  );
});
