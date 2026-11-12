import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { useVisiblePinBodies } from "./use-visible-pin-bodies";

function rect(top: number, bottom: number) { return { top, bottom, height: bottom - top, left: 0, right: 400, width: 400, x: 0, y: top, toJSON() {} }; }
afterEach(() => { document.body.replaceChildren(); vi.unstubAllGlobals(); });
it("rebinds virtualized bodies, excludes root wrappers and ignores overlay churn", async () => {
 let notify: () => void = () => {};
 const observe = vi.fn(); const unobserve = vi.fn(); const disconnect = vi.fn();
 vi.stubGlobal("IntersectionObserver", class { constructor(callback: () => void) { notify = callback; } observe = observe; unobserve = unobserve; disconnect = disconnect; });
 const container = document.createElement("div");
 container.getBoundingClientRect = () => rect(0, 500);
 document.body.append(container);
 const wrapper = document.createElement("div"); wrapper.id = "comment-root";
 wrapper.getBoundingClientRect = () => rect(0, 1000); container.append(wrapper);
 const body = document.createElement("div"); body.dataset.commentPinBody = "reply";
 body.getBoundingClientRect = () => rect(100, 200); wrapper.append(body);
 const overlay = document.createElement("div"); overlay.dataset.commentPinsOverlay = ""; container.append(overlay);
 const { result, unmount } = renderHook(() => useVisiblePinBodies(container));
 await waitFor(() => expect([...result.current]).toEqual(["reply"]));
 expect(observe).toHaveBeenCalledWith(body); expect(observe).not.toHaveBeenCalledWith(wrapper);
 act(() => { body.remove(); });
 await waitFor(() => expect(result.current.size).toBe(0));
 expect(unobserve).toHaveBeenCalledWith(body);
 const next = document.createElement("div"); next.dataset.commentPinBody = "reply";
 let top = 700; next.getBoundingClientRect = () => rect(top, top + 100);
 act(() => wrapper.append(next));
 await waitFor(() => expect(observe).toHaveBeenCalledWith(next));
 expect(result.current.size).toBe(0);
 act(() => { top = 100; notify(); });
 await waitFor(() => expect([...result.current]).toEqual(["reply"]));
 const stable = result.current;
 act(() => overlay.append(document.createElement("button")));
 await new Promise(resolve => setTimeout(resolve, 30));
 expect(result.current).toBe(stable);
 unmount(); expect(disconnect).toHaveBeenCalled();
});

it("treats content under a sticky composer as hidden, including composers mounted later", async () => {
 const observeSize = vi.fn();
 vi.stubGlobal("IntersectionObserver", class { observe() {} unobserve() {} disconnect() {} });
 vi.stubGlobal("ResizeObserver", class { observe = observeSize; unobserve() {} disconnect() {} });
 const container = document.createElement("div");
 container.getBoundingClientRect = () => rect(0, 500);
 document.body.append(container);
 const body = document.createElement("div"); body.dataset.commentPinBody = "c";
 body.getBoundingClientRect = () => rect(400, 450); container.append(body);
 const { result } = renderHook(() => useVisiblePinBodies(container));
 await waitFor(() => expect([...result.current]).toEqual(["c"]));
 const composer = document.createElement("div"); composer.dataset.commentPinsBottomOverlay = "";
 composer.style.position = "sticky"; composer.getBoundingClientRect = () => rect(350, 500);
 act(() => container.append(composer));
 await waitFor(() => expect(result.current.size).toBe(0));
 expect(observeSize).toHaveBeenCalledWith(composer);
});
