import { afterEach, describe, expect, it, vi } from "vitest";
import {
  isEmbeddedInCostrict,
  postCostrictNavigateToSession,
} from "./costrict-bridge";

describe("costrict-bridge", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  describe("isEmbeddedInCostrict", () => {
    it("true when coStrictToken is injected", () => {
      vi.stubGlobal("window", {
        desktopAPI: { coStrictToken: "tok" },
        location: { search: "" },
        parent: {},
      } as unknown as Window);
      expect(isEmbeddedInCostrict()).toBe(true);
    });

    it("true when embedded query param is present", () => {
      const w = { location: { search: "?embedded=opencode" } } as unknown as Window;
      // parent === window so only the query param can trip it
      (w as unknown as { parent: Window }).parent = w;
      vi.stubGlobal("window", w);
      expect(isEmbeddedInCostrict()).toBe(true);
    });

    it("false when standalone (own frame, no token, no param)", () => {
      const w = { location: { search: "" }, desktopAPI: undefined } as unknown as Window;
      (w as unknown as { parent: Window }).parent = w;
      vi.stubGlobal("window", w);
      expect(isEmbeddedInCostrict()).toBe(false);
    });
  });

  describe("postCostrictNavigateToSession", () => {
    it("posts the navigate message to the parent when embedded", () => {
      const postMessage = vi.fn();
      const parent = { postMessage } as unknown as Window;
      vi.stubGlobal("window", { parent } as unknown as Window);

      postCostrictNavigateToSession({ sessionId: "s1", workDir: "/p/proj" });

      expect(postMessage).toHaveBeenCalledWith(
        {
          type: "multica:navigate",
          target: "session",
          sessionId: "s1",
          workDir: "/p/proj",
        },
        "*",
      );
    });

    it("no-ops when sessionId or workDir is missing", () => {
      const postMessage = vi.fn();
      const parent = { postMessage } as unknown as Window;
      vi.stubGlobal("window", { parent } as unknown as Window);

      postCostrictNavigateToSession({ sessionId: "", workDir: "/p" });
      postCostrictNavigateToSession({ sessionId: "s1", workDir: "" });

      expect(postMessage).not.toHaveBeenCalled();
    });

    it("no-ops when there is no parent frame (standalone)", () => {
      const postMessage = vi.fn();
      const w = { } as Record<string, unknown>;
      w.parent = w;
      w.postMessage = postMessage;
      vi.stubGlobal("window", w as unknown as Window);

      postCostrictNavigateToSession({ sessionId: "s1", workDir: "/p" });

      expect(postMessage).not.toHaveBeenCalled();
    });
  });
});
