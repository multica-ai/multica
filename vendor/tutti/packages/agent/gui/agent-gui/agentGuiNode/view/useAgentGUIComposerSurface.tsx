import { useCallback, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { AgentComposer, type AgentComposerProps } from "../AgentComposer";

/** The editor belongs to the detail pane; home and dock only supply its placement. */
export function useAgentGUIComposerSurface(props: AgentComposerProps) {
  const [host] = useState(() => {
    const element = document.createElement("div");
    element.style.display = "contents";
    return element;
  });
  const [mounted, setMounted] = useState(false);
  const focused = useRef<HTMLElement | null>(null);
  const slotStyle = useMemo(() => ({ display: "contents" }), []);
  const attach = useCallback((slot: HTMLDivElement | null) => {
    if (!slot) {
      const active = host.ownerDocument.activeElement;
      focused.current = active instanceof HTMLElement && host.contains(active) ? active : null;
      setMounted(false);
      return;
    }
    slot.appendChild(host);
    setMounted(true);
    if (focused.current?.isConnected) focused.current.focus({ preventScroll: true });
    focused.current = null;
  }, [host]);
  return {
    slot: <div ref={attach} data-agent-composer-slot="" style={slotStyle} />,
    portal: mounted ? createPortal(<AgentComposer {...props} />, host) : null
  };
}
