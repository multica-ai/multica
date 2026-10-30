"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import {
  ArrowLeft,
  ArrowRight,
  Crosshair,
  Eraser,
  ExternalLink,
  Globe,
  MoveUpRight,
  Pencil,
  RotateCw,
  Send,
  Square,
  Trash2,
  Undo2,
  X,
} from "lucide-react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { attachmentToDraftUpload } from "@multica/core/drafts/draft-upload";
import { useCommentDraftStore } from "@multica/core/issues/stores";
import { useIssueBrowserStore } from "@multica/core/issues/stores/issue-browser-store";
import { Button } from "@multica/ui/components/ui/button";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { Input } from "@multica/ui/components/ui/input";
import { cn } from "@multica/ui/lib/utils";
import { attachmentMarkdown } from "../../editor/use-coordinated-uploads";
import { useT } from "../../i18n";

// Mirrors apps/desktop/src/shared/issue-browser.ts. Shared views cannot import
// from an app, and the bridge is absent on web, so the surface is typed here.
type Bounds = { x: number; y: number; width: number; height: number };
type BrowserState = {
  url: string;
  title: string;
  loading: boolean;
  canGoBack: boolean;
  canGoForward: boolean;
};
type PickedElement = {
  selector: string;
  tag: string;
  text: string;
  pageUrl: string;
  pageTitle: string;
};
type DrawTool = "pen" | "rect" | "arrow" | "eraser";
type Result<T> = { ok: true; value: T } | { ok: false; error?: string };
type Capture = { pngBase64: string; pageUrl: string; pageTitle: string };

interface IssueBrowserBridge {
  show: (id: string, b: Bounds, url?: string) => Promise<Result<BrowserState>>;
  setBounds: (id: string, b: Bounds) => Promise<Result<boolean>>;
  hide: (id: string) => Promise<Result<boolean>>;
  navigate: (id: string, url: string) => Promise<Result<boolean>>;
  control: (id: string, a: "back" | "forward" | "reload" | "stop") => Promise<Result<boolean>>;
  pickElement: (id: string) => Promise<Result<PickedElement | null>>;
  cancelPick: (id: string) => Promise<Result<boolean>>;
  draw: (
    id: string,
    c:
      | { type: "tool"; tool: DrawTool | null }
      | { type: "color"; color: string }
      | { type: "undo" }
      | { type: "clear" },
  ) => Promise<Result<{ canUndo: boolean }>>;
  capture: (id: string) => Promise<Result<Capture>>;
  onState: (cb: (s: BrowserState & { issueId: string }) => void) => () => void;
}

function getBridge(): IssueBrowserBridge | null {
  if (typeof window === "undefined") return null;
  const w = window as unknown as { desktopAPI?: { issueBrowser?: IssueBrowserBridge } };
  return w.desktopAPI?.issueBrowser ?? null;
}

const COLORS = ["#ef4444", "#f59e0b", "#22c55e", "#3b82f6"];

function pngFile(base64: string): File {
  const bin = atob(base64);
  const bytes = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
  return new File([bytes], `browser-${Date.now()}.png`, { type: "image/png" });
}

const OVERLAY_SELECTOR = [
  "dialog",
  "alert-dialog",
  "sheet",
  "popover",
  "dropdown-menu",
  "context-menu",
  "select",
]
  // keepMounted content (e.g. the running-agent chip popover) stays in the DOM
  // while closed; only count content that is open.
  .map((n) => `[data-slot="${n}-content"][data-open]`)
  .join(",");

function toBounds(el: HTMLElement): Bounds {
  const r = el.getBoundingClientRect();
  return { x: r.x, y: r.y, width: r.width, height: r.height };
}

/** Header toggle. Renders nothing where there is no embedded browser (web). */
export function IssueBrowserToggle({ issueId }: { issueId: string }) {
  const { t } = useT("issues");
  const open = useIssueBrowserStore((s) => !!s.openIssueIds[issueId]);
  const toggle = useIssueBrowserStore((s) => s.toggle);
  if (!getBridge()) return null;
  return (
    <IconButton
      label={open ? t(($) => $.browser.close) : t(($) => $.browser.open)}
      tooltipSide="bottom"
      variant={open ? "secondary" : "ghost"}
      className={open ? "" : "text-muted-foreground"}
      onClick={() => toggle(issueId)}
    >
      <Globe />
    </IconButton>
  );
}

/**
 * Right-hand browser column. The page itself is a native view owned by the
 * desktop main process, keyed by issue and kept alive while hidden, so closing
 * the panel or switching issues and coming back finds the same page.
 */
export function IssueBrowserPanel({ issueId }: { issueId: string }) {
  const open = useIssueBrowserStore((s) => !!s.openIssueIds[issueId]);
  if (!open || !getBridge()) return null;
  return <IssueBrowserPanelBody key={issueId} issueId={issueId} />;
}

function IssueBrowserPanelBody({ issueId }: { issueId: string }) {
  const bridge = getBridge()!;
  const { t } = useT("issues");
  const close = useIssueBrowserStore((s) => s.close);
  const width = useIssueBrowserStore((s) => s.width);
  const setWidth = useIssueBrowserStore((s) => s.setWidth);
  const dragRef = useRef<{ startX: number; startWidth: number } | null>(null);
  const viewportRef = useRef<HTMLDivElement>(null);

  // The floating chat launcher and window are anchored to the whole content
  // area; this keeps them clear of the panel, in the issue content region.
  useEffect(() => {
    const root = document.documentElement;
    root.style.setProperty("--issue-browser-inset", `${width}px`);
    return () => {
      root.style.removeProperty("--issue-browser-inset");
    };
  }, [width]);
  const [state, setState] = useState<BrowserState | null>(null);
  const [address, setAddress] = useState("");
  const [editingAddress, setEditingAddress] = useState(false);
  const [selecting, setSelecting] = useState(false);
  const [picked, setPicked] = useState<PickedElement | null>(null);
  const [tool, setTool] = useState<DrawTool | null>(null);
  const [color, setColor] = useState(COLORS[0]!);
  const [note, setNote] = useState("");
  const [sending, setSending] = useState(false);

  // Attach the native view over the viewport placeholder and keep it aligned.
  useEffect(() => {
    const el = viewportRef.current;
    if (!el) return;
    let last = toBounds(el);
    let raf = 0;
    let alive = true;
    void bridge.show(issueId, last).then((res) => {
      if (alive && res.ok) setState(res.value);
    });
    // A native view always paints above the DOM, so detach it while any
    // overlay (dialog, popover, menu, sheet) is open, and restore it after.
    let covered = false;
    const syncCovered = () => {
      const now = !!document.querySelector(OVERLAY_SELECTOR);
      if (now === covered) return;
      covered = now;
      if (covered) void bridge.hide(issueId);
      else void bridge.show(issueId, (last = toBounds(el)));
    };
    const mo = new MutationObserver(syncCovered);
    mo.observe(document.body, {
      childList: true,
      subtree: true,
      attributes: true,
      attributeFilter: ["data-open"],
    });
    syncCovered();
    const tick = () => {
      if (covered) {
        raf = requestAnimationFrame(tick);
        return;
      }
      const next = toBounds(el);
      if (
        next.x !== last.x || next.y !== last.y ||
        next.width !== last.width || next.height !== last.height
      ) {
        last = next;
        void bridge.setBounds(issueId, next);
      }
      raf = requestAnimationFrame(tick);
    };
    raf = requestAnimationFrame(tick);
    const off = bridge.onState((s) => {
      if (s.issueId === issueId) setState(s);
    });
    return () => {
      alive = false;
      cancelAnimationFrame(raf);
      mo.disconnect();
      off();
      void bridge.hide(issueId);
    };
  }, [bridge, issueId]);

  useEffect(() => {
    if (!editingAddress && state) setAddress(state.url === "about:blank" ? "" : state.url);
  }, [state, editingAddress]);

  const pickElement = useCallback(async () => {
    if (selecting) {
      void bridge.cancelPick(issueId);
      return;
    }
    setSelecting(true);
    try {
      await bridge.draw(issueId, { type: "tool", tool: null });
      setTool(null);
      const res = await bridge.pickElement(issueId);
      if (res.ok && res.value) setPicked(res.value);
    } finally {
      setSelecting(false);
    }
  }, [bridge, issueId, selecting]);

  const chooseTool = (next: DrawTool) => {
    const value = tool === next ? null : next;
    setTool(value);
    void bridge.draw(issueId, { type: "tool", tool: value });
  };

  // Populates the issue's comment composer; the user reviews and sends it there.
  const send = async () => {
    setSending(true);
    try {
      const cap = await bridge.capture(issueId);
      if (!cap.ok) throw new Error(cap.error ?? t(($) => $.browser.capture_failed));
      const attachment = await api.uploadFile(pngFile(cap.value.pngBase64), { issueId });
      const lines = [`[${cap.value.pageTitle || cap.value.pageUrl}](${cap.value.pageUrl})`];
      if (picked) {
        lines.push(`${t(($) => $.browser.selected_element)}: \`${picked.selector}\``);
        if (picked.text) {
          lines.push(
            picked.text
              .slice(0, 300)
              .split("\n")
              .map((l) => `> ${l}`)
              .join("\n"),
          );
        }
        if (note.trim()) lines.push(note.trim());
      }
      lines.push(attachmentMarkdown(attachment));
      const draftKey = `new:${issueId}` as const;
      const drafts = useCommentDraftStore.getState();
      drafts.addUpload(draftKey, attachmentToDraftUpload(attachment));
      drafts.appendToDraftContent(draftKey, lines.join("\n\n"));
      toast.success(t(($) => $.browser.sent));
      setPicked(null);
      setNote("");
      await bridge.draw(issueId, { type: "clear" });
    } catch (err) {
      toast.error(err instanceof Error && err.message ? err.message : t(($) => $.browser.send_failed));
    } finally {
      setSending(false);
    }
  };

  const toolButton = (id: DrawTool, label: string, icon: React.ReactNode) => (
    <IconButton
      key={id}
      label={label}
      size="icon-xs"
      variant={tool === id ? "secondary" : "ghost"}
      onClick={() => chooseTool(id)}
    >
      {icon}
    </IconButton>
  );

  return (
    <aside
      className="relative flex h-full max-w-[80%] shrink-0 flex-col border-l bg-background"
      style={{ width }}
    >
      {/* Pointer capture keeps the drag alive while the cursor crosses the native page view. */}
      <div
        role="separator"
        aria-orientation="vertical"
        aria-label={t(($) => $.browser.resize)}
        className="absolute inset-y-0 -left-2 z-10 w-2 cursor-col-resize touch-none hover:bg-border active:bg-border"
        onPointerDown={(e) => {
          e.currentTarget.setPointerCapture(e.pointerId);
          dragRef.current = { startX: e.clientX, startWidth: width };
        }}
        onPointerMove={(e) => {
          const drag = dragRef.current;
          if (drag) setWidth(drag.startWidth + drag.startX - e.clientX);
        }}
        onPointerUp={() => {
          dragRef.current = null;
        }}
        onPointerCancel={() => {
          dragRef.current = null;
        }}
      />
      <div className="flex items-center gap-1 border-b px-2 py-1.5">
        <IconButton label={t(($) => $.browser.back)} size="icon-xs" disabled={!state?.canGoBack} onClick={() => void bridge.control(issueId, "back")}>
          <ArrowLeft />
        </IconButton>
        <IconButton label={t(($) => $.browser.forward)} size="icon-xs" disabled={!state?.canGoForward} onClick={() => void bridge.control(issueId, "forward")}>
          <ArrowRight />
        </IconButton>
        <IconButton
          label={state?.loading ? t(($) => $.browser.stop) : t(($) => $.browser.reload)}
          size="icon-xs"
          onClick={() => void bridge.control(issueId, state?.loading ? "stop" : "reload")}
        >
          {state?.loading ? <X /> : <RotateCw />}
        </IconButton>
        <form
          className="min-w-0 flex-1"
          onSubmit={(e) => {
            e.preventDefault();
            void bridge.navigate(issueId, address);
            setEditingAddress(false);
          }}
        >
          <Input
            value={address}
            placeholder={t(($) => $.browser.url_placeholder)}
            aria-label={t(($) => $.browser.url_placeholder)}
            className="h-7"
            onFocus={(e) => {
              setEditingAddress(true);
              e.currentTarget.select();
            }}
            onBlur={() => setEditingAddress(false)}
            onChange={(e) => setAddress(e.target.value)}
          />
        </form>
        <IconButton
          label={t(($) => $.browser.open_external)}
          size="icon-xs"
          disabled={!state?.url.startsWith("http")}
          onClick={() => {
            const w = window as unknown as { desktopAPI?: { openExternal?: (u: string) => void } };
            if (state) w.desktopAPI?.openExternal?.(state.url);
          }}
        >
          <ExternalLink />
        </IconButton>
        <IconButton label={t(($) => $.browser.close)} size="icon-xs" onClick={() => close(issueId)}>
          <X />
        </IconButton>
      </div>

      <div className="flex items-center gap-1 border-b px-2 py-1">
        <IconButton
          label={t(($) => $.browser.select)}
          size="icon-xs"
          variant={selecting ? "secondary" : "ghost"}
          onClick={() => void pickElement()}
        >
          <Crosshair />
        </IconButton>
        <span className="mx-1 h-4 w-px bg-border" />
        {toolButton("pen", t(($) => $.browser.pen), <Pencil />)}
        {toolButton("rect", t(($) => $.browser.rect), <Square />)}
        {toolButton("arrow", t(($) => $.browser.arrow), <MoveUpRight />)}
        {toolButton("eraser", t(($) => $.browser.eraser), <Eraser />)}
        <span className="mx-1 h-4 w-px bg-border" />
        {COLORS.map((c) => (
          <button
            key={c}
            type="button"
            aria-label={t(($) => $.browser.color)}
            aria-pressed={color === c}
            className={cn(
              "size-4 rounded-full border",
              color === c && "ring-2 ring-ring ring-offset-1 ring-offset-background",
            )}
            style={{ backgroundColor: c }}
            onClick={() => {
              setColor(c);
              void bridge.draw(issueId, { type: "color", color: c });
            }}
          />
        ))}
        <span className="mx-1 h-4 w-px bg-border" />
        <IconButton label={t(($) => $.browser.undo)} size="icon-xs" onClick={() => void bridge.draw(issueId, { type: "undo" })}>
          <Undo2 />
        </IconButton>
        <IconButton label={t(($) => $.browser.clear)} size="icon-xs" onClick={() => void bridge.draw(issueId, { type: "clear" })}>
          <Trash2 />
        </IconButton>
        <IconButton label={t(($) => $.browser.send_drawing)} size="icon-xs" disabled={sending} onClick={() => void send()}>
          <Send />
        </IconButton>
      </div>

      {selecting && (
        <p className="border-b px-3 py-1 text-caption text-muted-foreground">
          {t(($) => $.browser.selecting)}
        </p>
      )}

      {picked && (
        <div className="flex items-center gap-1 border-b bg-muted/50 px-2 py-1.5">
          <Crosshair className="size-3 shrink-0 text-muted-foreground" />
          <code className="max-w-[30%] shrink-0 truncate text-caption">{picked.selector}</code>
          <Input
            value={note}
            placeholder={t(($) => $.browser.note_placeholder)}
            aria-label={t(($) => $.browser.note_placeholder)}
            className="h-7 min-w-0 flex-1"
            onChange={(e) => setNote(e.target.value)}
          />
          <Button size="xs" disabled={sending} aria-busy={sending} onClick={() => void send()}>
            {t(($) => $.browser.send)}
          </Button>
          <IconButton label={t(($) => $.browser.discard)} size="icon-xs" onClick={() => { setPicked(null); setNote(""); }}>
            <X />
          </IconButton>
        </div>
      )}

      {/* The native page view is positioned over this placeholder. */}
      <div ref={viewportRef} className="min-h-0 flex-1" />

    </aside>
  );
}
