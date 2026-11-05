import { Button } from "@multica/ui/components/ui/button";
import { DropdownMenuItem } from "@tutti-os/ui-system";
import { Copy,MessagesSquare,PanelRightOpen } from "lucide-react";
import {
createContext,
useContext,
useEffect,
useLayoutEffect,
useRef,
useState,
type ReactNode
} from "react";
import {
localResourceTarget,
previewResourceHref
} from "./message-resource-links";
import type { AgentGuiHostServices } from "./protocol";
import type { createMulticaAgentGuiRuntime } from "./runtime";

type Entry = {
  client: AgentGuiHostServices;
  handle: ReturnType<typeof createMulticaAgentGuiRuntime>;
  sessionId: string | null;
  chinese: boolean;
  enabled: boolean;
  sidePane?: boolean;
};
const SideEntryContext = createContext<Entry | null>(null);

export function useIsSidePane(): boolean {
  return useContext(SideEntryContext)?.sidePane === true;
}

export function useCanPreviewResources(): boolean {
  return useContext(SideEntryContext)?.enabled === true;
}

export function SideEntryProvider({
  children,
  root,
  ...entry
}: Entry & { root: HTMLElement; children: ReactNode }) {
  const current = useRef(entry);
  current.current = entry;
  const [selection, setSelection] = useState<{
    text: string;
    turnId?: string;
    left: number;
    top: number;
    bottom: number;
  } | null>(null);
  const toolbar = useRef<HTMLDivElement>(null);
  const [pending, setPending] = useState(false);
  useEffect(() => {
    if (!entry.enabled) return;
    const select = () => {
      const value = window.getSelection();
      const anchor = value?.anchorNode?.parentElement;
      const focus = value?.focusNode?.parentElement;
      const source =
        anchor && root.contains(anchor)
          ? anchor.closest<HTMLElement>("[data-multica-turn-id]")
          : null;
      const text = value?.toString().trim();
      if (
        !text ||
        !source ||
        !source.contains(focus ?? null) ||
        anchor?.closest('[contenteditable="true"],input,textarea') ||
        !current.current.sessionId ||
        !value?.rangeCount
      ) {
        setSelection(null);
        return;
      }
      const box = value.getRangeAt(0).getBoundingClientRect();
      setSelection({
        text: text.slice(0, 20000),
        turnId: source.dataset.multicaTurnId || undefined,
        left: box.left,
        top: box.top,
        bottom: box.bottom
      });
    };
    const report = (error: unknown) => {
      void current.current.client.hostApi.showMessage({
        message: error instanceof Error ? error.message : String(error),
        tone: "error"
      });
    };
    const key = (event: KeyboardEvent) => {
      if (event.key === "Escape") setSelection(null);
      else if (event.key === "Shift") select();
    };
    const open = (event: MouseEvent | KeyboardEvent) => {
      if (
        event.defaultPrevented ||
        (event instanceof KeyboardEvent && event.key !== "Enter") ||
        (event instanceof MouseEvent && event.button !== 0) ||
        !current.current.sessionId ||
        !(event.target instanceof Element)
      )
        return;
      const element = event.target.closest("a[href],[data-agent-link-href]");
      if (!element || !root.contains(element)) return;
      const url =
        element?.getAttribute("data-agent-link-href") ??
        element?.getAttribute("href");
      if (!url || !previewResourceHref(url)) return;
      event.preventDefault();
      event.stopPropagation();
      void current.current.client.hostApi
        .openResource({ sessionId: current.current.sessionId, url })
        .catch(report);
    };
    const contextMenu = (event: MouseEvent) => {
      if (
        event.defaultPrevented ||
        !current.current.sessionId ||
        !(event.target instanceof Element) ||
        event.target.closest('input,textarea,[contenteditable="true"],img')
      )
        return;
      const link = event.target.closest("a[href]");
      if (!link || !root.contains(link)) return;
      const url =
        link?.getAttribute("data-agent-link-href") ??
        link?.getAttribute("href");
      if (!url || !localResourceTarget(url)) return;
      event.preventDefault();
      event.stopPropagation();
      void current.current.client.hostApi
        .showFileContextMenu({
          sessionId: current.current.sessionId,
          url,
          hasSelection: Boolean(window.getSelection()?.toString().trim())
        })
        .catch(report);
    };
    document.addEventListener("contextmenu", contextMenu, true);
    document.addEventListener("pointerup", select);
    document.addEventListener("keyup", key);
    document.addEventListener("scroll", select, true);
    document.addEventListener("click", open, true);
    document.addEventListener("keydown", open, true);
    window.addEventListener("resize", select);
    return () => {
      document.removeEventListener("contextmenu", contextMenu, true);
      document.removeEventListener("pointerup", select);
      document.removeEventListener("keyup", key);
      document.removeEventListener("scroll", select, true);
      document.removeEventListener("click", open, true);
      document.removeEventListener("keydown", open, true);
      window.removeEventListener("resize", select);
    };
  }, [entry.enabled, root]);
  useLayoutEffect(() => {
    if (!selection || !toolbar.current) return;
    const { width, height } = toolbar.current.getBoundingClientRect();
    const above = selection.top - height - 8;
    toolbar.current.style.left = `${Math.max(8, Math.min(selection.left, window.innerWidth - width - 8))}px`;
    toolbar.current.style.top = `${Math.max(8, above >= 8 ? above : Math.min(selection.bottom + 8, window.innerHeight - height - 8))}px`;
  }, [selection, entry.chinese]);
  useEffect(() => {
    let cancelled = false;
    const release = entry.client.subscribe((event) => {
      if (event.type !== "locateSource") return;
      let attempts = 0;
      const find = () => {
        if (cancelled || ++attempts > 30) return;
        const source =
          current.current.sessionId === event.sessionId && event.turnId
            ? root.querySelector(
                `[data-multica-turn-id="${CSS.escape(event.turnId)}"]`
              )
            : null;
        const target = source && source.firstElementChild;
        if (target) {
          let anchor = target;
          if (event.quote && source) {
            const at = source.textContent?.indexOf(event.quote) ?? -1;
            if (at >= 0) {
              const walker = document.createTreeWalker(
                source,
                NodeFilter.SHOW_TEXT
              );
              let offset = 0;
              let node: Node | null;
              while ((node = walker.nextNode())) {
                if (offset + (node.textContent?.length ?? 0) > at) {
                  anchor = node.parentElement ?? target;
                  break;
                }
                offset += node.textContent?.length ?? 0;
              }
            }
          }
          anchor.scrollIntoView({ block: "center", behavior: "smooth" });
          anchor.setAttribute("data-multica-located", "true");
          setTimeout(
            () => anchor.removeAttribute("data-multica-located"),
            2000
          );
        } else {
          if (event.turnId) {
            root
              .querySelector<HTMLElement>(
                `[data-multica-locator-turn-id="${CSS.escape(event.turnId)}"]`
              )
              ?.click();
            // The turn may be older than what is loaded: read back until it is, or none are left.
            const source = current.current.handle.localConversations;
            const conversation = source?.conversation(event.sessionId);
            if (
              conversation &&
              !conversation.turns.has(event.turnId) &&
              conversation.hasOlderTurns
            ) {
              attempts = 0;
              void source?.loadOlder(event.sessionId).catch(() => {});
            }
          }
          setTimeout(find, 100);
        }
      };
      setTimeout(find, 100);
    });
    return () => {
      cancelled = true;
      release();
    };
  }, [entry.client]);
  useEffect(() => {
    setSelection(null);
  }, [entry.sessionId]);
  const dismissSelection = () => {
    setSelection(null);
    window.getSelection()?.removeAllRanges();
  };
  const copy = async () => {
    if (!selection) return;
    const { text } = selection;
    dismissSelection();
    try {
      await navigator.clipboard.writeText(text);
    } catch {
      await entry.client.hostApi.showMessage({
        message: entry.chinese
          ? "无法复制文字，请重试"
          : "Could not copy text. Try again.",
        tone: "error"
      });
      return;
    }
    await entry.client.hostApi.showMessage({
      message: entry.chinese ? "复制成功" : "Copied successfully",
      tone: "success"
    });
  };
  const start = async () => {
    if (!selection || !entry.sessionId || pending) return;
    const { text, turnId } = selection;
    dismissSelection();
    setPending(true);
    try {
      await entry.client.hostApi.openSide({
        sessionId: entry.sessionId,
        turnId,
        quote: text,
        requestId: crypto.randomUUID()
      });
    } catch (error) {
      await entry.client.hostApi.showMessage({
        message: error instanceof Error ? error.message : String(error),
        tone: "error"
      });
    } finally {
      setPending(false);
    }
  };
  return (
    <SideEntryContext.Provider value={entry}>
      {children}
      {selection && (
        <div
          ref={toolbar}
          role="toolbar"
          aria-label={entry.chinese ? "选中文字操作" : "Selected text actions"}
          className="multica-selection-toolbar nodrag fixed z-[100]"
          style={{ left: selection.left, top: selection.top }}
          onPointerDown={(event) => event.preventDefault()}
        >
          <Button
            size="xs"
            variant="ghost"
            onClick={() => {
              void copy();
            }}
          >
            <Copy className="size-3.5" />
            {entry.chinese ? "复制" : "Copy"}
          </Button>
          <Button
            size="xs"
            variant="ghost"
            disabled={pending}
            aria-busy={pending}
            onClick={() => {
              void start();
            }}
          >
            <MessagesSquare className="size-3.5" />
            {entry.chinese ? "在旁支中询问" : "Ask in side"}
          </Button>
        </div>
      )}
    </SideEntryContext.Provider>
  );
}

export function SideQueueAction({
  agentSessionId,
  promptId,
  disabled
}: {
  agentSessionId?: string | null;
  promptId: string;
  disabled?: boolean;
}) {
  const entry = useContext(SideEntryContext);
  const [pending, setPending] = useState(false);
  if (!entry?.enabled || !agentSessionId) return null;
  return (
    <DropdownMenuItem
      className="multica-queue-side-action nodrag"
      disabled={disabled || pending}
      onSelect={() => {
        setPending(true);
        void entry.handle
          .moveQueuedToSide(agentSessionId, promptId)
          .catch((error: unknown) =>
            entry.client.hostApi.showMessage({
              message: error instanceof Error ? error.message : String(error),
              tone: "error"
            })
          )
          .finally(() => setPending(false));
      }}
    >
      <PanelRightOpen size={16} aria-hidden="true" />
      <div className="multica-queue-action-copy">
        <span className="multica-queue-action-title">
          {entry.chinese ? "在旁支中执行" : "Run in side"}
        </span>
        <span className="text-caption text-muted-foreground">
          {entry.chinese
            ? "移出主队列，在右侧独立执行"
            : "Move from this queue and run alongside"}
        </span>
      </div>
    </DropdownMenuItem>
  );
}
