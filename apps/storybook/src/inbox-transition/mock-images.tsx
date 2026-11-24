import { useEffect, useState } from "react";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { cn } from "@multica/ui/lib/utils";
import type { Lang } from "../voice-input/copy";

export type MockImageKind = "perf-chart" | "ui-compare" | "ci-matrix" | "trace-waterfall";

function svgToDataUri(svg: string): string {
  return `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`;
}

const IMAGE_DATA_URI: Record<MockImageKind, string> = {
  "perf-chart": svgToDataUri(`<svg xmlns="http://www.w3.org/2000/svg" width="640" height="340" viewBox="0 0 640 340">
    <rect width="640" height="340" rx="12" fill="#0f172a"/>
    <text x="28" y="38" fill="#e2e8f0" font-family="system-ui,sans-serif" font-size="14" font-weight="600">Inbox Switch Latency &amp; CLS Breakdown (n=50)</text>
    <text x="28" y="58" fill="#94a3b8" font-family="system-ui,sans-serif" font-size="11">Cold cache vs. Anchor prefetch + reserved image aspect ratio</text>
    <g stroke="#1e293b" stroke-width="1">
      <line x1="28" y1="95" x2="612" y2="95"/><line x1="28" y1="150" x2="612" y2="150"/>
      <line x1="28" y1="205" x2="612" y2="205"/><line x1="28" y1="260" x2="612" y2="260"/>
    </g>
    <rect x="64" y="112" width="68" height="148" rx="6" fill="#ef4444" fill-opacity="0.85"/>
    <rect x="148" y="218" width="68" height="42" rx="6" fill="#3b82f6"/>
    <rect x="274" y="138" width="68" height="122" rx="6" fill="#ef4444" fill-opacity="0.85"/>
    <rect x="358" y="238" width="68" height="22" rx="6" fill="#3b82f6"/>
    <rect x="484" y="168" width="68" height="92" rx="6" fill="#f59e0b" fill-opacity="0.9"/>
    <rect x="568" y="254" width="32" height="6" rx="3" fill="#10b981"/>
    <g fill="#cbd5e1" font-family="system-ui,sans-serif" font-size="11" text-anchor="middle">
      <text x="98" y="104">480ms</text><text x="182" y="210">92ms</text>
      <text x="308" y="130">340ms</text><text x="392" y="230">44ms</text>
      <text x="518" y="160">0.21 CLS</text><text x="584" y="246">0.00</text>
      <text x="140" y="284" fill="#94a3b8">Cold Switch</text>
      <text x="350" y="284" fill="#94a3b8">Warm Anchor Jump</text>
      <text x="545" y="284" fill="#94a3b8">Image Shift</text>
    </g>
    <g font-family="system-ui,sans-serif" font-size="11">
      <rect x="28" y="306" width="10" height="10" rx="2" fill="#ef4444"/><text x="44" y="315" fill="#94a3b8">Current</text>
      <rect x="112" y="306" width="10" height="10" rx="2" fill="#3b82f6"/><text x="128" y="315" fill="#94a3b8">Prefetched + Steady Chrome</text>
      <rect x="298" y="306" width="10" height="10" rx="2" fill="#10b981"/><text x="314" y="315" fill="#94a3b8">Reserved Aspect Ratio</text>
    </g>
  </svg>`),
  "ui-compare": svgToDataUri(`<svg xmlns="http://www.w3.org/2000/svg" width="640" height="340" viewBox="0 0 640 340">
    <rect width="640" height="340" rx="12" fill="#18181b"/>
    <text x="24" y="34" fill="#f4f4f5" font-family="system-ui,sans-serif" font-size="13" font-weight="600">InboxBar 40px Slot Stability — Before vs After</text>
    <rect x="24" y="52" width="284" height="260" rx="8" fill="#27272a" stroke="#3f3f46"/>
    <rect x="332" y="52" width="284" height="260" rx="8" fill="#27272a" stroke="#3b82f6"/>
    <text x="40" y="76" fill="#fca5a5" font-family="system-ui,sans-serif" font-size="11" font-weight="600">Before: Bar unmounts (-40px jump)</text>
    <rect x="40" y="90" width="252" height="26" rx="4" fill="#3f3f46"/>
    <rect x="40" y="122" width="252" height="24" rx="4" fill="#7f1d1d" stroke="#ef4444" stroke-dasharray="4 3"/>
    <text x="166" y="138" fill="#fecaca" font-family="system-ui,sans-serif" font-size="10" text-anchor="middle">Missing 40px InboxBar during switch</text>
    <rect x="40" y="154" width="190" height="14" rx="3" fill="#52525b"/>
    <rect x="40" y="176" width="230" height="10" rx="3" fill="#3f3f46"/>
    <rect x="40" y="194" width="210" height="10" rx="3" fill="#3f3f46"/>
    <rect x="40" y="220" width="252" height="72" rx="6" fill="#18181b"/>
    <text x="348" y="76" fill="#93c5fd" font-family="system-ui,sans-serif" font-size="11" font-weight="600">After: Locked 40px bar + 2px progress</text>
    <rect x="348" y="90" width="252" height="26" rx="4" fill="#3f3f46"/>
    <rect x="348" y="122" width="252" height="24" rx="4" fill="#1e293b" stroke="#334155"/>
    <rect x="348" y="144" width="168" height="2" fill="#3b82f6"/>
    <text x="360" y="138" fill="#cbd5e1" font-family="system-ui,sans-serif" font-size="10">Changed to Done · opening next...</text>
    <rect x="348" y="158" width="190" height="14" rx="3" fill="#71717a"/>
    <rect x="348" y="180" width="230" height="10" rx="3" fill="#52525b"/>
    <rect x="348" y="198" width="210" height="10" rx="3" fill="#52525b"/>
    <rect x="348" y="220" width="252" height="72" rx="6" fill="#1e293b" stroke="#3b82f6" stroke-opacity="0.5"/>
  </svg>`),
  "ci-matrix": svgToDataUri(`<svg xmlns="http://www.w3.org/2000/svg" width="640" height="320" viewBox="0 0 640 320">
    <rect width="640" height="320" rx="12" fill="#09090b"/>
    <text x="26" y="36" fill="#fafafa" font-family="system-ui,sans-serif" font-size="14" font-weight="600">Playwright Visual &amp; E2E Verification Snapshot</text>
    <rect x="26" y="56" width="588" height="44" rx="8" fill="#18181b" stroke="#27272a"/>
    <circle cx="48" cy="78" r="7" fill="#10b981"/><text x="66" y="82" fill="#e4e4e7" font-family="system-ui,sans-serif" font-size="12">inbox-jump.spec.ts — warm p50: 46ms, longest task: 24ms</text>
    <rect x="26" y="110" width="588" height="44" rx="8" fill="#18181b" stroke="#27272a"/>
    <circle cx="48" cy="132" r="7" fill="#10b981"/><text x="66" y="136" fill="#e4e4e7" font-family="system-ui,sans-serif" font-size="12">inbox-page.test.tsx — 64/64 passed (archived + live + undo)</text>
    <rect x="26" y="164" width="588" height="44" rx="8" fill="#18181b" stroke="#27272a"/>
    <circle cx="48" cy="186" r="7" fill="#10b981"/><text x="66" y="190" fill="#e4e4e7" font-family="system-ui,sans-serif" font-size="12">issue-inbox-actions.test.tsx — settled bar + auto-archive buffer</text>
    <rect x="26" y="218" width="588" height="72" rx="8" fill="#172554" fill-opacity="0.45" stroke="#1d4ed8"/>
    <text x="44" y="246" fill="#93c5fd" font-family="ui-monospace,monospace" font-size="11">scrollTop before image load: 3420px</text>
    <text x="44" y="268" fill="#93c5fd" font-family="ui-monospace,monospace" font-size="11">scrollTop after 3 images load: 3420px (delta: 0px)</text>
  </svg>`),
  "trace-waterfall": svgToDataUri(`<svg xmlns="http://www.w3.org/2000/svg" width="640" height="320" viewBox="0 0 640 320">
    <rect width="640" height="320" rx="12" fill="#111827"/>
    <text x="26" y="34" fill="#f9fafb" font-family="system-ui,sans-serif" font-size="13" font-weight="600">Network &amp; Render Waterfall — Auto-Advance Transition</text>
    <g font-family="ui-monospace,monospace" font-size="11" fill="#9ca3af">
      <text x="26" y="74">GET /issues/MUL-9806</text>
      <text x="26" y="114">GET /issues/.../timeline</text>
      <text x="26" y="154">GET /issues/.../tasks</text>
      <text x="26" y="194">IMG screenshot-1.png</text>
      <text x="26" y="234">IMG screenshot-2.png</text>
      <text x="26" y="274">Scroll anchor align</text>
    </g>
    <rect x="210" y="60" width="120" height="18" rx="4" fill="#3b82f6"/>
    <rect x="210" y="100" width="195" height="18" rx="4" fill="#6366f1"/>
    <rect x="210" y="140" width="148" height="18" rx="4" fill="#8b5cf6"/>
    <rect x="405" y="180" width="110" height="18" rx="4" fill="#f59e0b"/>
    <rect x="405" y="220" width="165" height="18" rx="4" fill="#f59e0b"/>
    <rect x="405" y="260" width="18" height="18" rx="4" fill="#10b981"/>
    <text x="580" y="233" fill="#fbbf24" font-family="system-ui,sans-serif" font-size="10">+480px CLS</text>
    <text x="430" y="273" fill="#34d399" font-family="system-ui,sans-serif" font-size="10">pre-paint align</text>
  </svg>`),
};

export function DelayedCommentImage({
  kind,
  caption,
  delayMs,
  reserveAspectRatio,
  loadedInitially,
  onExpandedWithoutReserve,
  lang,
}: {
  kind: MockImageKind;
  caption: Record<Lang, string>;
  delayMs: number;
  reserveAspectRatio: boolean;
  loadedInitially: boolean;
  onExpandedWithoutReserve?: (heightPx: number) => void;
  lang: Lang;
}) {
  const [loaded, setLoaded] = useState(loadedInitially || delayMs <= 0);

  useEffect(() => {
    if (loadedInitially || delayMs <= 0) {
      setLoaded(true);
      return;
    }
    setLoaded(false);
    const id = window.setTimeout(() => {
      setLoaded(true);
      if (!reserveAspectRatio) onExpandedWithoutReserve?.(340);
    }, delayMs);
    return () => window.clearTimeout(id);
  }, [delayMs, loadedInitially, reserveAspectRatio, onExpandedWithoutReserve]);

  // Current (A): unreserved image starts at 0px height and suddenly expands to 240px,
  // pushing the target comment below it right out of the reader's gaze.
  if (!reserveAspectRatio && !loaded) {
    return (
      <div className="mt-2 text-micro text-faint-foreground">
        {lang === "zh" ? "图片加载中…" : "Loading image…"}
      </div>
    );
  }

  return (
    <figure className="mt-3 overflow-hidden rounded-lg border border-border bg-muted/40">
      <div className="relative aspect-[640/340] w-full overflow-hidden bg-muted/60">
        {!loaded && (
          <div className="absolute inset-0 flex flex-col justify-between p-4">
            <Skeleton className="h-4 w-44" />
            <Skeleton className="flex-1 my-3 w-full rounded-md" />
            <Skeleton className="h-3 w-32" />
          </div>
        )}
        <img
          src={IMAGE_DATA_URI[kind]}
          alt={caption[lang]}
          className={cn(
            "h-full w-full object-contain transition-opacity duration-200",
            loaded ? "opacity-100" : "opacity-0",
          )}
        />
      </div>
      <figcaption className="flex items-center justify-between border-t border-border/60 px-3 py-1.5 text-micro text-muted-foreground">
        <span className="truncate">{caption[lang]}</span>
        <span className="shrink-0 tabular-nums text-faint-foreground">
          {loaded
            ? reserveAspectRatio
              ? "CLS 0px"
              : lang === "zh"
                ? "展开 +340px"
                : "+340px shift"
            : `${delayMs}ms`}
        </span>
      </figcaption>
    </figure>
  );
}
