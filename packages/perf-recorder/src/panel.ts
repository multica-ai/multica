import type { Recorder, LiveStatus } from "./recorder";
import type { Incident, InteractionType } from "./types";

// The panel is built with plain DOM inside a Shadow root — deliberately NOT
// React. That keeps the recorder free of a React dependency AND means the panel
// produces zero React commits and its internal DOM mutations are encapsulated
// in the shadow tree, so the recorder never observes itself (MUL-4466 §10).
const HOST_ID = "multica-perf-recorder";

const STYLE = `
:host { all: initial; }
.root { position: fixed; right: 16px; bottom: 16px; z-index: 2147483000;
  font: 12px/1.4 ui-sans-serif, system-ui, sans-serif; color: #e5e7eb; }
.badge { display: flex; align-items: center; gap: 6px; background: #111827;
  border: 1px solid #374151; border-radius: 999px; padding: 6px 12px; cursor: pointer;
  box-shadow: 0 4px 12px rgba(0,0,0,.4); }
.dot { width: 8px; height: 8px; border-radius: 50%; background: #6b7280; }
.dot.rec { background: #ef4444; }
.panel { width: 380px; max-height: 60vh; display: flex; flex-direction: column;
  background: #0b1120; border: 1px solid #374151; border-radius: 10px; overflow: hidden;
  box-shadow: 0 8px 28px rgba(0,0,0,.5); }
.bar { display: flex; align-items: center; gap: 6px; padding: 8px 10px; border-bottom: 1px solid #1f2937; }
.bar button { background: #1f2937; color: #e5e7eb; border: 1px solid #374151; border-radius: 6px;
  padding: 4px 8px; cursor: pointer; font: inherit; }
.bar button:hover { background: #374151; }
.stats { margin-left: auto; display: flex; gap: 10px; color: #9ca3af; font-variant-numeric: tabular-nums; }
.chips { display: flex; gap: 6px; padding: 6px 10px; border-bottom: 1px solid #1f2937; flex-wrap: wrap; }
.chip { background: #111827; border: 1px solid #374151; border-radius: 999px; padding: 2px 8px; cursor: pointer; color: #9ca3af; }
.chip.on { background: #2563eb; color: white; border-color: #2563eb; }
.list { overflow: auto; }
.row { padding: 8px 10px; border-bottom: 1px solid #111827; cursor: pointer; }
.row:hover { background: #111827; }
.row .top { display: flex; gap: 8px; align-items: baseline; }
.row .dur { margin-left: auto; font-variant-numeric: tabular-nums; }
.sev-ok { color: #34d399; } .sev-warn { color: #fbbf24; } .sev-bad { color: #f87171; }
.muted { color: #6b7280; }
.detail { margin-top: 6px; padding: 6px 8px; background: #0f172a; border-radius: 6px; color: #cbd5e1; white-space: pre-wrap; }
.empty { padding: 16px; text-align: center; color: #6b7280; }
`;

const CHIPS: Array<{ key: InteractionType | "all"; label: string }> = [
  { key: "all", label: "All" },
  { key: "click", label: "Click" },
  { key: "scroll", label: "Scroll" },
  { key: "input", label: "Input" },
  { key: "navigation", label: "Nav" },
  { key: "background", label: "Background" },
];

export interface PanelHandle {
  destroy: () => void;
}

export function mountPanel(recorder: Recorder): PanelHandle {
  if (typeof document === "undefined") {
    return { destroy: () => {} };
  }
  const existing = document.getElementById(HOST_ID);
  if (existing) existing.remove();

  const host = document.createElement("div");
  host.id = HOST_ID;
  const shadow = host.attachShadow({ mode: "open" });
  const style = document.createElement("style");
  style.textContent = STYLE;
  shadow.appendChild(style);
  const root = document.createElement("div");
  root.className = "root";
  shadow.appendChild(root);
  document.body.appendChild(host);

  let expanded = false;
  let filter: InteractionType | "all" = "all";
  let openId: string | null = null;
  let status: LiveStatus = {
    state: "idle",
    fps: 0,
    jankCount: 0,
    longTaskCount: 0,
    durationMs: 0,
    incidentCount: 0,
  };
  let incidents: Incident[] = [];

  const render = () => {
    if (!expanded) {
      root.innerHTML = "";
      const badge = el("div", "badge");
      badge.appendChild(el("span", `dot ${status.state === "recording" ? "rec" : ""}`));
      badge.appendChild(text("span", `Perf · ${status.incidentCount}`));
      badge.onclick = () => {
        expanded = true;
        render();
      };
      root.appendChild(badge);
      return;
    }

    root.innerHTML = "";
    const panel = el("div", "panel");

    const bar = el("div", "bar");
    bar.appendChild(el("span", `dot ${status.state === "recording" ? "rec" : ""}`));
    bar.appendChild(button(status.state === "recording" ? "Stop" : "Start", () => {
      if (status.state === "recording") recorder.stop();
      else recorder.start();
    }));
    bar.appendChild(button("Clear", () => recorder.clear()));
    bar.appendChild(button("Export", () => downloadReport(recorder)));
    bar.appendChild(button("×", () => {
      expanded = false;
      render();
    }));
    const stats = el("div", "stats");
    stats.appendChild(text("span", `${status.fps}fps`));
    stats.appendChild(text("span", `jank ${status.jankCount}`));
    stats.appendChild(text("span", `${Math.round(status.durationMs / 100) / 10}s`));
    bar.appendChild(stats);
    panel.appendChild(bar);

    const chips = el("div", "chips");
    for (const c of CHIPS) {
      const chip = text("span", c.label + countFor(c.key, incidents));
      chip.className = `chip ${filter === c.key ? "on" : ""}`;
      chip.onclick = () => {
        filter = c.key;
        render();
      };
      chips.appendChild(chip);
    }
    panel.appendChild(chips);

    const list = el("div", "list");
    const shown = incidents
      .filter((i) => filter === "all" || i.interaction.type === filter)
      .slice()
      .reverse();
    if (shown.length === 0) {
      list.appendChild(text("div", "No incidents yet. Start recording and interact.", "empty"));
    }
    for (const incident of shown) {
      list.appendChild(renderRow(incident, openId, (id) => {
        openId = openId === id ? null : id;
        render();
      }));
    }
    panel.appendChild(list);
    root.appendChild(panel);
  };

  const offStatus = recorder.onStatus((s) => {
    status = s;
    render();
  });
  const offIncidents = recorder.onIncidents((list) => {
    incidents = list;
    render();
  });
  render();

  return {
    destroy: () => {
      offStatus();
      offIncidents();
      host.remove();
    },
  };
}

function renderRow(incident: Incident, openId: string | null, toggle: (id: string) => void): HTMLElement {
  const row = el("div", "row");
  const top = el("div", "top");
  top.appendChild(text("span", incident.interaction.type));
  top.appendChild(text("span", incident.route.pathname ?? "—", "muted"));
  const dur = text("span", `${incident.totalDurationMs}ms`, `dur ${severity(incident.totalDurationMs)}`);
  top.appendChild(dur);
  row.appendChild(top);
  row.appendChild(text("div", primaryLabel(incident), "muted"));
  if (openId === incident.id) {
    row.appendChild(text("div", detailText(incident), "detail"));
  }
  row.onclick = () => toggle(incident.id);
  return row;
}

function primaryLabel(incident: Incident): string {
  switch (incident.primaryEvidence) {
    case "react_commit":
      return "Slow React commit";
    case "long_task":
      return "Long task on main thread";
    case "frame":
      return "Dropped / long frame";
    case "resource":
      return "Slow resource";
    case "interaction":
      return "Slow interaction";
    default:
      return "Insufficient evidence — inspect in Chrome Performance";
  }
}

function detailText(incident: Incident): string {
  const lines: string[] = [];
  if (incident.interaction.testId) lines.push(`testId: ${incident.interaction.testId}`);
  lines.push(`mutations: ${incident.mutationCount}`);
  for (const c of incident.reactCommits)
    lines.push(`react ${c.boundaryId ?? "(unregistered)"} ${c.phase} ${c.actualDurationMs}ms`);
  for (const l of incident.longTasks) lines.push(`longTask ${l.durationMs}ms @${l.startOffsetMs}`);
  for (const f of incident.frames) lines.push(`frame ${f.durationMs}ms (${f.source})`);
  for (const r of incident.resources)
    lines.push(`resource ${r.durationMs}ms ${r.origin ?? ""}${r.pathname ?? ""}`);
  return lines.join("\n");
}

function countFor(key: InteractionType | "all", incidents: Incident[]): string {
  const n = key === "all" ? incidents.length : incidents.filter((i) => i.interaction.type === key).length;
  return n > 0 ? ` ${n}` : "";
}

function severity(ms: number): string {
  if (ms >= 500) return "sev-bad";
  if (ms >= 100) return "sev-warn";
  return "sev-ok";
}

export function downloadReport(recorder: Recorder): void {
  if (typeof document === "undefined") return;
  const report = recorder.export();
  const blob = new Blob([JSON.stringify(report, null, 2)], { type: "application/json" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = `perf-recorder-${report.recorderVersion}.json`;
  a.click();
  URL.revokeObjectURL(url);
}

function el(tag: string, className = ""): HTMLElement {
  const node = document.createElement(tag);
  if (className) node.className = className;
  return node;
}

function text(tag: string, content: string, className = ""): HTMLElement {
  const node = el(tag, className);
  node.textContent = content;
  return node;
}

function button(label: string, onClick: () => void): HTMLElement {
  const b = el("button");
  b.textContent = label;
  b.onclick = onClick;
  return b;
}
