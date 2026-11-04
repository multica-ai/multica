/// <reference lib="webworker" />

// The office runs here, off the page's thread: the engine steps, the frame is
// painted to the canvas, and the tags' positions go back to the page. A busy
// page then never stalls the office, and a busy office never stalls the page.

import init, { Office } from "./wasm/pixtuoid_web.js";
import {
  FRAME_MS,
  LAYOUT_SEED,
  type FromWorker,
  type OfficeLabel,
  type OfficePoint,
  type OfficeRoom,
  type OfficeUnits,
  type ToWorker,
  type WorkerSession,
} from "./office-protocol";

const scope = self as unknown as DedicatedWorkerGlobalScope;

let office: Office | null = null;
let memory: WebAssembly.Memory | null = null;
let canvas: OffscreenCanvas | null = null;
let context: OffscreenCanvasRenderingContext2D | null = null;
let paused = false;
// Unknown until the page has measured itself; the office waits for it.
let units: OfficeUnits | null = null;
let sessions: readonly WorkerSession[] = [];
// The workers this office has seated: the room each sits in, and whether it works.
const seated = new Map<string, { room: number; working: boolean }>();
// Workers moved to another room: each has left, and walks in to its new room once it is gone.
const moving = new Map<string, number>();
let lastLabels = "";
let lastRooms = "";

function post(message: FromWorker): void {
  scope.postMessage(message);
}

/** Brings the office in line with the sessions: seats new workers, moves them between rooms, sends finished ones home. */
function reconcile(view: Office): void {
  const wanted = new Map(sessions.map((session) => [session.key, session]));
  for (const key of [...seated.keys()]) {
    if (wanted.has(key)) continue;
    view.issue_end(key);
    seated.delete(key);
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
    }
    if (moving.has(key)) {
      if (view.issue_present(key)) continue;
      // A full room has no desk left: the worker waits outside until one frees up.
      if (!view.issue_start(key, want.room, false)) continue;
      moving.delete(key);
      seated.set(key, { room: want.room, working: false });
    } else if (!seated.has(key)) {
      // A full office has no desk left: the worker waits until one frees up.
      if (!view.issue_start(key, want.room, true)) continue;
      seated.set(key, { room: want.room, working: false });
    }
    const current = seated.get(key);
    if (!current || current.working === want.working) continue;
    view.issue_working(key, want.working);
    current.working = want.working;
  }
}

/**
 * Paints the engine's frame. The view wraps wasm memory directly, so nothing is copied per frame.
 * Only the rects the engine reports as changed are sent to the canvas: a settled office sends none.
 */
function paint(view: Office, wasm: WebAssembly.Memory, size: OfficeUnits): void {
  if (!canvas || !context) return;
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
  // Read the views after every step: a grown memory detaches the older ones.
  const pixels = new Uint8ClampedArray(wasm.buffer, view.frame_ptr(), view.frame_len());
  const image = new ImageData(pixels, width, height);
  if (resized || view.dirty_full()) {
    context.putImageData(image, 0, 0);
    return;
  }
  const rects = new Uint32Array(wasm.buffer, view.dirty_ptr(), view.dirty_len());
  for (let i = 0; i < rects.length; i += 4) {
    const [x = 0, y = 0, w = 0, h = 0] = rects.subarray(i, i + 4);
    context.putImageData(image, 0, 0, x, y, w, h);
  }
}

function frame(): void {
  if (!office || !memory) return;
  if (!paused && units) {
    office.step(Date.now(), units.w, units.h);
    reconcile(office);
    paint(office, memory, units);
    const overlay = JSON.parse(office.overlay_json()) as {
      labels: OfficeLabel[];
      rooms: OfficeRoom[];
      tank: OfficePoint | null;
      whiteboards: OfficeRoom[];
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
  scope.setTimeout(frame, FRAME_MS);
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
      office = new Office(LAYOUT_SEED);
      office.clear_script();
      // The sky follows the clock, sunny by day and dark by night, without the rain and fog of the engine's weather cycle.
      office.set_weather("clear");
      frame();
    })
    .catch(() => post({ type: "failed" }));
};

