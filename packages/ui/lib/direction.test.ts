import { afterEach, describe, expect, it } from "vitest";
import {
  getInlineScroll,
  inlineSign,
  isRtl,
  setInlineScroll,
} from "./direction";

function mount(dir?: "ltr" | "rtl") {
  const host = document.createElement("div");
  if (dir) host.setAttribute("dir", dir);
  const element = document.createElement("div");
  host.appendChild(element);
  document.body.appendChild(host);
  return element;
}

afterEach(() => {
  document.body.innerHTML = "";
});

describe("direction helpers", () => {
  it("reads the direction from the nearest dir attribute", () => {
    expect(isRtl(mount())).toBe(false);
    expect(isRtl(mount("ltr"))).toBe(false);
    expect(isRtl(mount("rtl"))).toBe(true);
    expect(inlineSign(mount())).toBe(1);
    expect(inlineSign(mount("rtl"))).toBe(-1);
  });

  it("lets an LTR island inside an RTL page stay left-to-right", () => {
    const page = mount("rtl");
    const island = document.createElement("div");
    island.setAttribute("dir", "ltr");
    page.appendChild(island);
    expect(isRtl(island)).toBe(false);
  });

  it("measures scroll from the inline start in both directions", () => {
    const ltr = mount("ltr");
    ltr.scrollLeft = 40;
    expect(getInlineScroll(ltr)).toBe(40);

    // Browsers report RTL scrollLeft as 0 at the start and negative toward the end.
    const rtl = mount("rtl");
    Object.defineProperty(rtl, "scrollLeft", { value: -40, writable: true });
    expect(getInlineScroll(rtl)).toBe(40);

    setInlineScroll(rtl, 25);
    expect(rtl.scrollLeft).toBe(-25);
    setInlineScroll(ltr, 25);
    expect(ltr.scrollLeft).toBe(25);
  });
});
