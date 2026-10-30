// Scripts injected into the guest page of the issue browser. They run in the
// page's main world via webContents.executeJavaScript, so they must stay
// self-contained and must not trust or expose anything privileged: the guest
// page is untrusted, and results are treated as plain data by the caller.

/**
 * Element picker. Resolves with the clicked element's description, or null
 * when cancelled (Escape or `window.__multicaPickCancel()`).
 */
export const PICK_ELEMENT_SCRIPT = `new Promise((resolve) => {
  if (window.__multicaPickCancel) window.__multicaPickCancel();
  const box = document.createElement("div");
  box.style.cssText =
    "position:fixed;z-index:2147483647;pointer-events:none;border:2px solid #3b82f6;background:rgba(59,130,246,0.15);transition:all 40ms;";
  document.documentElement.appendChild(box);
  const prevCursor = document.documentElement.style.cursor;
  document.documentElement.style.cursor = "crosshair";
  let current = null;
  const cssPath = (el) => {
    const parts = [];
    while (el && el.nodeType === 1 && parts.length < 6) {
      let part = el.tagName.toLowerCase();
      if (el.id) { parts.unshift(part + "#" + CSS.escape(el.id)); break; }
      const cls = Array.from(el.classList).slice(0, 2).map((c) => "." + CSS.escape(c)).join("");
      part += cls;
      const parent = el.parentElement;
      if (parent) {
        const same = Array.from(parent.children).filter((c) => c.tagName === el.tagName);
        if (same.length > 1) part += ":nth-of-type(" + (same.indexOf(el) + 1) + ")";
      }
      parts.unshift(part);
      el = parent;
    }
    return parts.join(" > ");
  };
  const onMove = (e) => {
    current = e.target;
    if (!(current instanceof Element)) return;
    const r = current.getBoundingClientRect();
    box.style.left = r.left + "px";
    box.style.top = r.top + "px";
    box.style.width = r.width + "px";
    box.style.height = r.height + "px";
  };
  const block = (e) => { e.preventDefault(); e.stopPropagation(); };
  const finish = (value) => {
    document.removeEventListener("mousemove", onMove, true);
    document.removeEventListener("click", onClick, true);
    document.removeEventListener("mousedown", block, true);
    document.removeEventListener("mouseup", block, true);
    document.removeEventListener("keydown", onKey, true);
    document.documentElement.style.cursor = prevCursor;
    box.remove();
    window.__multicaPickCancel = undefined;
    resolve(value);
  };
  const onClick = (e) => {
    block(e);
    const el = e.target;
    if (!(el instanceof Element)) return finish(null);
    const r = el.getBoundingClientRect();
    finish({
      selector: cssPath(el),
      tag: el.tagName.toLowerCase(),
      text: (el.innerText || el.textContent || "").trim().slice(0, 500),
      html: el.outerHTML.slice(0, 1500),
      pageUrl: location.href,
      pageTitle: document.title,
      rect: { x: r.left, y: r.top, width: r.width, height: r.height },
    });
  };
  const onKey = (e) => { if (e.key === "Escape") { block(e); finish(null); } };
  window.__multicaPickCancel = () => finish(null);
  document.addEventListener("mousemove", onMove, true);
  document.addEventListener("click", onClick, true);
  document.addEventListener("mousedown", block, true);
  document.addEventListener("mouseup", block, true);
  document.addEventListener("keydown", onKey, true);
})`;

export const PICK_CANCEL_SCRIPT = `void (window.__multicaPickCancel && window.__multicaPickCancel())`;

/** Installs (once) the drawing overlay and returns its controller state. */
const DRAW_BOOTSTRAP = `(() => {
  if (window.__multicaDraw) return window.__multicaDraw;
  const canvas = document.createElement("canvas");
  canvas.style.cssText = "position:fixed;inset:0;width:100vw;height:100vh;z-index:2147483646;pointer-events:none;";
  document.documentElement.appendChild(canvas);
  const ctx = canvas.getContext("2d");
  const ops = [];
  const s = { tool: null, color: "#ef4444", drawing: null };
  const fit = () => {
    const dpr = window.devicePixelRatio || 1;
    canvas.width = Math.round(window.innerWidth * dpr);
    canvas.height = Math.round(window.innerHeight * dpr);
    redraw();
  };
  const paint = (op) => {
    const dpr = window.devicePixelRatio || 1;
    ctx.save();
    ctx.scale(dpr, dpr);
    ctx.strokeStyle = op.color;
    ctx.fillStyle = op.color;
    ctx.lineWidth = 3;
    ctx.lineCap = "round";
    ctx.lineJoin = "round";
    if (op.kind === "pen") {
      ctx.beginPath();
      op.points.forEach((p, i) => (i ? ctx.lineTo(p[0], p[1]) : ctx.moveTo(p[0], p[1])));
      ctx.stroke();
    } else if (op.kind === "rect") {
      ctx.strokeRect(op.a[0], op.a[1], op.b[0] - op.a[0], op.b[1] - op.a[1]);
    } else if (op.kind === "arrow") {
      const [x1, y1] = op.a, [x2, y2] = op.b;
      const ang = Math.atan2(y2 - y1, x2 - x1);
      ctx.beginPath();
      ctx.moveTo(x1, y1); ctx.lineTo(x2, y2);
      ctx.moveTo(x2, y2); ctx.lineTo(x2 - 14 * Math.cos(ang - 0.45), y2 - 14 * Math.sin(ang - 0.45));
      ctx.moveTo(x2, y2); ctx.lineTo(x2 - 14 * Math.cos(ang + 0.45), y2 - 14 * Math.sin(ang + 0.45));
      ctx.stroke();
    }
    ctx.restore();
  };
  const redraw = () => {
    ctx.clearRect(0, 0, canvas.width, canvas.height);
    ops.forEach(paint);
    if (s.drawing) paint(s.drawing);
  };
  const eraseAt = (x, y) => {
    for (let i = ops.length - 1; i >= 0; i--) {
      const op = ops[i];
      const pts = op.kind === "pen" ? op.points : [op.a, op.b];
      const xs = pts.map((p) => p[0]), ys = pts.map((p) => p[1]);
      const pad = 10;
      if (x >= Math.min(...xs) - pad && x <= Math.max(...xs) + pad &&
          y >= Math.min(...ys) - pad && y <= Math.max(...ys) + pad) {
        ops.splice(i, 1);
        redraw();
        return;
      }
    }
  };
  const pos = (e) => [e.clientX, e.clientY];
  canvas.addEventListener("pointerdown", (e) => {
    if (!s.tool) return;
    canvas.setPointerCapture(e.pointerId);
    if (s.tool === "eraser") { eraseAt(e.clientX, e.clientY); s.erasing = true; return; }
    const p = pos(e);
    s.drawing = s.tool === "pen"
      ? { kind: "pen", color: s.color, points: [p] }
      : { kind: s.tool, color: s.color, a: p, b: p };
  });
  canvas.addEventListener("pointermove", (e) => {
    if (s.erasing) return eraseAt(e.clientX, e.clientY);
    if (!s.drawing) return;
    if (s.drawing.kind === "pen") s.drawing.points.push(pos(e));
    else s.drawing.b = pos(e);
    redraw();
  });
  const end = () => {
    s.erasing = false;
    if (s.drawing) { ops.push(s.drawing); s.drawing = null; redraw(); }
  };
  canvas.addEventListener("pointerup", end);
  canvas.addEventListener("pointercancel", end);
  window.addEventListener("resize", fit);
  fit();
  const api = {
    state: () => ({ tool: s.tool, color: s.color, canUndo: ops.length > 0 }),
    setTool: (tool) => { s.tool = tool; canvas.style.pointerEvents = tool ? "auto" : "none"; canvas.style.cursor = tool ? "crosshair" : "default"; },
    setColor: (color) => { s.color = color; },
    undo: () => { ops.pop(); redraw(); },
    clear: () => { ops.length = 0; redraw(); },
    hasInk: () => ops.length > 0,
  };
  window.__multicaDraw = api;
  return api;
})()`;

const draw = (call: string) =>
  `(() => { const d = ${DRAW_BOOTSTRAP}; ${call}; return d.state(); })()`;

export const drawStateScript = () => draw("");
export const drawSetToolScript = (tool: string | null) =>
  draw(`d.setTool(${JSON.stringify(tool)})`);
export const drawSetColorScript = (color: string) =>
  draw(`d.setColor(${JSON.stringify(color)})`);
export const DRAW_UNDO_SCRIPT = draw("d.undo()");
export const DRAW_CLEAR_SCRIPT = draw("d.clear()");
