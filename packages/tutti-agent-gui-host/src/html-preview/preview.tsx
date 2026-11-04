import { useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";
import { Button } from "@multica/ui/components/ui/button";
import { getPortalContainer } from "@tutti-os/ui-system/portal-container";
import { HTML_PREVIEW_MAX_LENGTH, type MessagePreviewFormat } from "../html-preview-contract";
import { createHtmlPreviewDocument, type PreviewLibraries, type PreviewPalette } from "./document";
import { SvgPreviewSurface } from "./svg-preview-surface";

function subscribeAppearance(changed: () => void) {
  const observer = new MutationObserver(changed);
  observer.observe(getPortalContainer(), { attributes: true, attributeFilter: ["data-theme", "lang"] });
  return () => observer.disconnect();
}
function appearance() { const root = getPortalContainer(); return `${root.dataset.theme}:${root.lang}`; }
const EMPTY_LIBRARIES: PreviewLibraries = { chart: "", d3: "" };

export function DocumentPreview({ source, streaming, format }: { source: string; streaming: boolean; format: MessagePreviewFormat }) {
  const currentAppearance = useSyncExternalStore(subscribeAppearance, appearance);
  const chinese = currentAppearance.split(":")[1]?.startsWith("zh");
  const theme = currentAppearance.startsWith("dark:") ? "dark" : "light";
  const [mode, setMode] = useState<"preview" | "source">("preview");
  const [large, setLarge] = useState(false);
  const [zoom, setZoom] = useState(1);
  const [attempt, setAttempt] = useState(0);
  const [copied, setCopied] = useState(false);
  const [libraries, setLibraries] = useState<PreviewLibraries | null>(null);
  const [loadError, setLoadError] = useState(false);
  const [ready, setReady] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const frame = useRef<HTMLIFrameElement>(null);
  const expandButton = useRef<HTMLButtonElement>(null);
  const copyTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const oversized = source.length > HTML_PREVIEW_MAX_LENGTH;
  const channel = useMemo(() => `${crypto.randomUUID()}:${source.length}:${attempt}:${theme}:${format}`, [source, attempt, theme, format]);
  const name = format.toUpperCase();
  const labels = chinese ? { preview: "预览", source: "源码", copy: `复制 ${name}`, copied: "已复制", expand: "展开预览", collapse: "收起预览", loading: "正在加载预览…", waiting: "生成完成后显示预览", error: `${name} 预览出错`, reload: format === "svg" ? "重新加载" : "重新运行", tooLarge: `${name} 内容过长，请查看源码`, failed: "无法加载图表库，请重试", copyFailed: "复制失败，请在源码中复制", timeout: "预览未响应，可查看源码或重新运行", zoomIn: "放大 SVG", zoomOut: "缩小 SVG", fit: "适应面板" }
    : { preview: "Preview", source: "Source", copy: `Copy ${name}`, copied: "Copied", expand: "Expand preview", collapse: "Collapse preview", loading: "Loading preview…", waiting: "Preview appears when generation finishes", error: `${name} preview error`, reload: format === "svg" ? "Reload" : "Run again", tooLarge: `${name} is too large to preview; view source`, failed: "Could not load chart libraries; retry", copyFailed: "Could not copy; copy from source", timeout: "Preview did not respond; view source or run again", zoomIn: "Zoom in SVG", zoomOut: "Zoom out SVG", fit: "Fit to panel" };
  const timeoutMessage = useRef(labels.timeout);
  timeoutMessage.current = labels.timeout;

  useEffect(() => {
    if (streaming || oversized) return;
    let current = true;
    setLoadError(false);
    if (format === "svg") return;
    void import("./libraries.generated").then((loaded) => { if (current) setLibraries(loaded.libraries); }, () => { if (current) setLoadError(true); });
    return () => { current = false; };
  }, [streaming, oversized, attempt, format]);
  useEffect(() => () => { if (copyTimer.current) clearTimeout(copyTimer.current); }, []);

  const documentSource = useMemo(() => {
    if ((format === "html" && !libraries) || streaming || oversized) return undefined;
    const style = getComputedStyle(getPortalContainer());
    const palette: PreviewPalette = {};
    for (const name of ["background", "foreground", "muted", "muted-foreground", "border", "brand"] as const) palette[name] = style.getPropertyValue(`--${name}`).trim();
    return createHtmlPreviewDocument(source, libraries ?? EMPTY_LIBRARIES, channel, theme, palette, format);
  }, [source, libraries, streaming, oversized, channel, theme, format]);

  useEffect(() => {
    setReady(false); setError(null);
    if (!documentSource) return;
    const timeout = setTimeout(() => setError(timeoutMessage.current), 10_000);
    const receive = (event: MessageEvent) => {
      if (event.source !== frame.current?.contentWindow || event.origin !== "null" || event.data?.type !== "multica:html-preview" || event.data.channel !== channel) return;
      if (event.data.kind === "ready") { setReady(true); clearTimeout(timeout); }
      if (event.data.kind === "error" && typeof event.data.message === "string") { clearTimeout(timeout); setReady(true); setError(event.data.message.slice(0, 500)); }
      if (format === "svg" && event.data.kind === "expand") setLarge(true);
      if (format === "svg" && event.data.kind === "collapse") setLarge(false);
    };
    window.addEventListener("message", receive);
    return () => { clearTimeout(timeout); window.removeEventListener("message", receive); };
  }, [documentSource, channel, format]);
  useEffect(() => {
    if (format === "svg" && ready) frame.current?.contentWindow?.postMessage({ type: "multica:svg-preview:zoom", channel, zoom, expanded: large, expandLabel: labels.expand }, "*");
  }, [format, ready, channel, zoom, large, labels.expand]);

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(source); setCopied(true);
      if (copyTimer.current) clearTimeout(copyTimer.current);
      copyTimer.current = setTimeout(() => setCopied(false), 1500);
    } catch { setError(labels.copyFailed); }
  };
  const expandedSvg = format === "svg" && large;
  const content = <section className={`multica-html-preview overflow-hidden rounded-lg border border-border bg-background text-foreground ${expandedSvg ? "flex h-full min-h-0 flex-col" : "my-4"}`} aria-label={name} data-format={format} data-testid="multica-html-preview">
    <div className={`flex shrink-0 flex-wrap items-center gap-1 border-b border-border bg-muted/50 px-3 py-2 ${expandedSvg ? "pr-12" : ""}`}>
      <span className="mr-auto text-caption font-medium">{name}{format === "html" && <span className="ml-2 font-normal text-muted-foreground">Chart.js · D3</span>}</span>
      <div role="group" aria-label={chinese ? `${name} 显示方式` : `${name} display`} className="flex gap-1">
        <Button variant={mode === "preview" ? "secondary" : "ghost"} size="xs" aria-pressed={mode === "preview"} onClick={() => setMode("preview")}>{labels.preview}</Button>
        <Button variant={mode === "source" ? "secondary" : "ghost"} size="xs" aria-pressed={mode === "source"} onClick={() => setMode("source")}>{labels.source}</Button>
      </div>
      {format === "svg" && <div role="group" aria-label={chinese ? "SVG 缩放" : "SVG zoom"} className="flex gap-1">
        <Button variant="ghost" size="xs" aria-label={labels.zoomOut} disabled={!ready || zoom <= 0.25} onClick={() => setZoom((value) => Math.max(0.25, value - 0.25))}>−</Button>
        <Button variant="ghost" size="xs" aria-label={labels.fit} disabled={!ready} onClick={() => setZoom(1)}>{Math.round(zoom * 100)}%</Button>
        <Button variant="ghost" size="xs" aria-label={labels.zoomIn} disabled={!ready || zoom >= 4} onClick={() => setZoom((value) => Math.min(4, value + 0.25))}>+</Button>
      </div>}
      <Button variant="ghost" size="xs" onClick={() => { void copy(); }}>{copied ? labels.copied : labels.copy}</Button>
      <Button ref={expandButton} variant="ghost" size="xs" aria-expanded={large} onClick={() => setLarge((value) => !value)}>{large ? labels.collapse : labels.expand}</Button>
      {!streaming && <Button variant="ghost" size="xs" onClick={() => setAttempt((value) => value + 1)}>{labels.reload}</Button>}
    </div>
    {(error || loadError) && <div role="alert" className="border-b border-border px-3 py-2 text-caption text-destructive">{labels.error}：{loadError ? labels.failed : error}</div>}
    {mode === "source" && <pre className="m-0 max-h-96 overflow-auto whitespace-pre-wrap break-words p-4 text-caption" data-testid="multica-html-source"><code>{source}</code></pre>}
    <div hidden={mode !== "preview"} className={expandedSvg ? "relative min-h-0 flex-1" : "relative"}>
      {(streaming || oversized || (!error && !loadError && (!documentSource || !ready))) && <p role="status" className="px-4 py-3 text-caption text-muted-foreground">{streaming ? labels.waiting : oversized ? labels.tooLarge : labels.loading}</p>}
      {documentSource && <iframe ref={frame} key={channel} name={`multica-html-preview:${channel}`} title={chinese ? `${name} 预览` : `${name} preview`} sandbox="allow-scripts" referrerPolicy="no-referrer" allow="camera 'none'; microphone 'none'; geolocation 'none'; clipboard-read 'none'; clipboard-write 'none'" srcDoc={documentSource} className="block w-full border-0" style={{ height: expandedSvg ? "100%" : large ? "min(70vh, 720px)" : 360 }} />}
    </div>
  </section>;
  return format === "svg" ? <SvgPreviewSurface open={large} onOpenChange={setLarge} title={chinese ? "SVG 预览" : "SVG preview"} closeLabel={chinese ? "关闭预览" : "Close preview"} trigger={expandButton}>{content}</SvgPreviewSurface> : content;
}
