import { afterEach, describe, expect, it } from "vitest";
import { isRecorderEvent } from "../src/self-surface";
import { RECORDER_HOST_ID } from "../src/constants";

function makeHost(): HTMLElement {
  const host = document.createElement("div");
  host.id = RECORDER_HOST_ID;
  document.body.appendChild(host);
  return host;
}

/** A minimal Event stand-in — isRecorderEvent only reads target + composedPath. */
function ev(target: Node | null, path: EventTarget[] = []): Event {
  return { target, composedPath: () => path } as unknown as Event;
}

afterEach(() => {
  document.body.innerHTML = "";
});

describe("isRecorderEvent", () => {
  it("matches when composedPath includes the recorder host (real-browser path)", () => {
    const host = makeHost();
    const inner = document.createElement("button");
    expect(isRecorderEvent(ev(inner, [inner, host, document.body, window]))).toBe(true);
  });

  it("matches a retargeted event whose target IS the host", () => {
    const host = makeHost();
    expect(isRecorderEvent(ev(host))).toBe(true);
  });

  it("matches a light-DOM descendant of the host via the ancestor walk", () => {
    const host = makeHost();
    const child = document.createElement("span");
    host.appendChild(child);
    expect(isRecorderEvent(ev(child))).toBe(true);
  });

  it("matches a shadow-DOM descendant by hopping across the shadow host", () => {
    const host = makeHost();
    const shadow = host.attachShadow({ mode: "open" });
    const inner = document.createElement("button");
    shadow.appendChild(inner);
    expect(isRecorderEvent(ev(inner))).toBe(true);
  });

  it("does not match events outside the recorder surface", () => {
    const other = document.createElement("button");
    document.body.appendChild(other);
    expect(isRecorderEvent(ev(other, [other, document.body]))).toBe(false);
  });
});
