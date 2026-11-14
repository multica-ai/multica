import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  BACKDROP_ATTR,
  BACKDROP_ELEMENT_ID,
  backdropImages,
  removeBackdrop,
  syncBackdrop,
  type BackdropState,
} from "./backdrop-layer";

const NIGHT = 'url("data:image/svg+xml;base64,bmlnaHQ=") center / cover no-repeat';
const DAWN = 'url("data:image/svg+xml;base64,ZGF3bg==") center / cover no-repeat';

/** Image decodes that finish only when the test says so. */
const decodes = new Map<string, () => void>();

function variables(values: Record<string, string>) {
  return (name: string) => values[name] ?? "";
}

const image = () => document.getElementById(BACKDROP_ELEMENT_ID)?.firstElementChild as HTMLElement | null;
const settle = () => new Promise((resolve) => setTimeout(resolve, 0));
const finishDecode = async (value: string) => {
  for (const url of backdropImages(value)) decodes.get(url)?.();
  await settle();
};

describe("backdrop layer", () => {
  let state: BackdropState;

  beforeEach(() => {
    state = { request: 0 };
    decodes.clear();
    // jsdom has no HTMLImageElement.decode.
    Object.defineProperty(HTMLImageElement.prototype, "decode", {
      configurable: true,
      value(this: HTMLImageElement) {
        return new Promise<void>((resolve) => decodes.set(this.src, resolve));
      },
    });
  });

  afterEach(() => {
    removeBackdrop(state);
    delete (HTMLImageElement.prototype as { decode?: unknown }).decode;
  });

  it("finds the images in a background value", () => {
    expect(backdropImages(`${NIGHT}, linear-gradient(#000, #111), url(a.png)`)).toEqual([
      "data:image/svg+xml;base64,bmlnaHQ=",
      "a.png",
    ]);
  });

  it("paints the backdrop once it has decoded and marks <html>", async () => {
    syncBackdrop(state, variables({ "--backdrop": NIGHT }));
    expect(document.documentElement.hasAttribute(BACKDROP_ATTR)).toBe(true);
    expect(document.body.firstElementChild?.id).toBe(BACKDROP_ELEMENT_ID);
    expect(image()?.style.background).toBe("");
    await finishDecode(NIGHT);
    expect(image()?.style.background).toContain("bmlnaHQ=");
  });

  it("keeps the old backdrop on screen until the new one has decoded", async () => {
    syncBackdrop(state, variables({ "--backdrop": NIGHT }));
    await finishDecode(NIGHT);
    syncBackdrop(state, variables({ "--backdrop": DAWN }));
    await settle();
    expect(image()?.style.background).toContain("bmlnaHQ=");
    await finishDecode(DAWN);
    expect(image()?.style.background).toContain("ZGF3bg==");
  });

  it("drops a decode that a later change overtook", async () => {
    syncBackdrop(state, variables({ "--backdrop": NIGHT }));
    syncBackdrop(state, variables({ "--backdrop": DAWN }));
    await finishDecode(DAWN);
    await finishDecode(NIGHT);
    expect(image()?.style.background).toContain("ZGF3bg==");
  });

  it("removes the layer when the mode has no backdrop", () => {
    syncBackdrop(state, variables({ "--backdrop": NIGHT }));
    syncBackdrop(state, variables({ "--backdrop": " none" }));
    expect(document.getElementById(BACKDROP_ELEMENT_ID)).toBeNull();
    expect(document.documentElement.hasAttribute(BACKDROP_ATTR)).toBe(false);
  });
});
