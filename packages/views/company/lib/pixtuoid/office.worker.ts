/// <reference lib="webworker" />

// The company runs here, off the page's thread: the engine steps, the frame is
// painted to the canvas, and the tags' positions go back to the page. A busy
// page then never stalls the company, and a busy company never stalls the page.

import init, { Company } from "./wasm/pixtuoid_web.js";
import {
  FRAME_MS,
  LAYOUT_SEED,
  type FromWorker,
  type CompanyLabel,
  type CompanyPoint,
  type CompanyRoom,
  type CompanyUnits,
  type ToWorker,
  type WorkerSession,
} from "./office-protocol";

const scope = self as unknown as DedicatedWorkerGlobalScope;

let company: Company | null = null;
let memory: WebAssembly.Memory | null = null;
let canvas: OffscreenCanvas | null = null;
let context: OffscreenCanvasRenderingContext2D | null = null;
let paused = false;
// Unknown until the page has measured itself; the company waits for it.
let units: CompanyUnits | null = null;
let sessions: readonly WorkerSession[] = [];
// The workers this company has seated: the room each sits in, and whether it works.
const seated = new Map<string, { room: number; working: boolean }>();
// Workers moved to another room: each has left, and walks in to its new room once it is gone.
const moving = new Map<string, number>();
let lastLabels = "";
let lastRooms = "";

function post(message: FromWorker): void {
  scope.postMessage(message);
}

/**
 * Brings the company in line with the sessions: seats new workers, moves them between rooms, sends finished ones home.
 * Returns whether it changed anything, so a frame with nothing new skips the overlay.
 */
function reconcile(view: Company): boolean {
  let changed = false;
  const wanted = new Map(sessions.map((session) => [session.key, session]));
  for (const key of [...seated.keys()]) {
    if (wanted.has(key)) continue;
    view.issue_end(key);
    seated.delete(key);
    changed = true;
  }
  for (const key of [...moving.keys()]) {
    if (!wanted.has(key)) moving.delete(key);
  }
  for (const [key, want] of wanted) {
    const seat = seated.get(key);
    if (seat && seat.room !== want.room) {
      // A worker leaves its desk through the door, and walks in to the new room only once it is gone.
      view.issue_end(key);
      seated.delete(key);
      moving.set(key, want.room);
      changed = true;
    }
    if (moving.has(key)) {
      if (view.issue_present(key)) continue;
      // A full room has no desk left: the worker waits outside until one frees up.
      if (!view.issue_start(key, want.room, false)) continue;
      moving.delete(key);
      seated.set(key, { room: want.room, working: false });
      changed = true;
    } else if (!seated.has(key)) {
      // A full company has no desk left: the worker waits until one frees up.
      if (!view.issue_start(key, want.room, true)) continue;
      seated.set(key, { room: want.room, working: false });
      changed = true;
    }
    const current = seated.get(key);
    if (!current || current.working === want.working) continue;
    view.issue_working(key, want.working);
    current.working = want.working;
    changed = true;
  }
  return changed;
}

/**
 * Paints the engine's frame. The view wraps wasm memory directly, so nothing is copied per frame.
 * Only the rects the engine reports as changed are sent to the canvas: a settled company sends none.
 * Returns whether the canvas was painted.
 */
function paint(view: Company, wasm: WebAssembly.Memory, size: CompanyUnits): boolean {
  if (!canvas || !context) return false;
  const scale = view.scale();
  const width = size.w * scale;
  const height = size.h * scale;
  let resized = false;
  if (canvas.width !== width) {
    canvas.width = width;
    resized = true;
  }
  if (canvas.height !== height) {
    canvas.height = height;
    resized = true;
  }
  const full = resized || view.dirty_full();
  if (!full && view.dirty_len() === 0) return false;
  // Read the views after every step: a grown memory detaches the older ones.
  const pixels = new Uint8ClampedArray(wasm.buffer, view.frame_ptr(), view.frame_len());
  const image = new ImageData(pixels, width, height);
  if (full) {
    context.putImageData(image, 0, 0);
    return true;
  }
  const rects = new Uint32Array(wasm.buffer, view.dirty_ptr(), view.dirty_len());
  for (let i = 0; i < rects.length; i += 4) {
    const [x = 0, y = 0, w = 0, h = 0] = rects.subarray(i, i + 4);
    context.putImageData(image, 0, 0, x, y, w, h);
  }
  return true;
}

// The overlay is re-read when the picture or the seating changes, and at least this often as a safety net.
const OVERLAY_EVERY_FRAMES = 30;
let framesSinceOverlay = OVERLAY_EVERY_FRAMES;

function frame(): void {
  if (!company || !memory) return;
  if (!paused && units) {
    company.step(Date.now(), units.w, units.h);
    const reseated = reconcile(company);
    const painted = paint(company, memory, units);
    framesSinceOverlay += 1;
    // A settled company paints nothing and seats nobody, so its tags and rooms cannot have moved.
    if (painted || reseated || framesSinceOverlay >= OVERLAY_EVERY_FRAMES) {
      framesSinceOverlay = 0;
      publishOverlay(company);
    }
  }
  scope.setTimeout(frame, FRAME_MS);
}

/** Sends the tags and the layout to the page, each only when it changed. */
function publishOverlay(view: Company): void {
  const overlay = JSON.parse(view.overlay_json()) as {
    labels: CompanyLabel[];
    rooms: CompanyRoom[];
    tank: CompanyPoint | null;
    whiteboards: CompanyRoom[];
  };
  const encoded = JSON.stringify(overlay.labels);
  if (encoded !== lastLabels) {
    lastLabels = encoded;
    post({ type: "labels", labels: overlay.labels });
  }
  const layout = JSON.stringify([overlay.rooms, overlay.tank, overlay.whiteboards]);
  if (layout !== lastRooms) {
    lastRooms = layout;
    post({ type: "rooms", rooms: overlay.rooms, tank: overlay.tank, boards: overlay.whiteboards });
  }
}

scope.onmessage = (event: MessageEvent<ToWorker>) => {
  const message = event.data;
  if (message.type === "sessions") {
    sessions = message.sessions;
    return;
  }
  if (message.type === "size") {
    units = message.units;
    return;
  }
  if (message.type === "paused") {
    paused = message.paused;
    return;
  }
  canvas = message.canvas;
  context = canvas.getContext("2d");
  init()
    .then((exports) => {
      memory = exports.memory;
      company = new Company(LAYOUT_SEED);
      company.clear_script();
      // One still daytime sky under a cool haze: no night, no weather cycle, and the outside is drawn once.
      company.set_daylight("fog");
      frame();
    })
    .catch(() => post({ type: "failed" }));
};

