import { describe, expect, it } from "vitest";
import type { Agent } from "@multica/core/types";
import {
  isAgentVisibleOnSurface,
  isAgentHiddenOnSurface,
  isFullySurfaceHidden,
  surfaceVisibilityPreset,
  presetToSurfaceMap,
} from "../views/surface-visibility";

const OWNER = "owner-1";
const OTHER = "other-1";

function agent(surface_visibility: Record<string, boolean> | null): Agent {
  return {
    id: "a1",
    owner_id: OWNER,
    visibility: "private",
    surface_visibility,
  } as unknown as Agent;
}

describe("surface visibility", () => {
  it("defaults to visible when field is unset", () => {
    const a = agent(null);
    expect(
      isAgentVisibleOnSurface(a, "lists", { userId: OTHER, role: "member" }),
    ).toBe(true);
  });

  it("hides a non-owner member on a surface set to false", () => {
    const a = agent({ lists: false });
    expect(
      isAgentHiddenOnSurface(a, "lists", { userId: OTHER, role: "member" }),
    ).toBe(true);
    // a different surface stays visible
    expect(
      isAgentHiddenOnSurface(a, "chat", { userId: OTHER, role: "member" }),
    ).toBe(false);
  });

  it("never hides the owner or workspace admins", () => {
    const a = agent({ lists: false, mention: false, chat: false, channels: false });
    expect(
      isAgentVisibleOnSurface(a, "lists", { userId: OWNER, role: "member" }),
    ).toBe(true);
    expect(
      isAgentVisibleOnSurface(a, "lists", { userId: OTHER, role: "admin" }),
    ).toBe(true);
    expect(
      isAgentVisibleOnSurface(a, "lists", { userId: OTHER, role: "owner" }),
    ).toBe(true);
  });

  it("classifies presets", () => {
    expect(surfaceVisibilityPreset(null)).toBe("visible");
    expect(surfaceVisibilityPreset({})).toBe("visible");
    expect(
      surfaceVisibilityPreset({ lists: false, mention: false, chat: false, channels: false }),
    ).toBe("hidden");
    expect(surfaceVisibilityPreset({ lists: false })).toBe("custom");
  });

  it("isFullySurfaceHidden only on all-false", () => {
    expect(isFullySurfaceHidden(null)).toBe(false);
    expect(isFullySurfaceHidden({ lists: false })).toBe(false);
    expect(
      isFullySurfaceHidden({ lists: false, mention: false, chat: false, channels: false }),
    ).toBe(true);
  });

  it("presetToSurfaceMap builds the right maps", () => {
    expect(presetToSurfaceMap("visible")).toEqual({});
    expect(presetToSurfaceMap("hidden")).toEqual({
      lists: false,
      mention: false,
      chat: false,
      channels: false,
    });
  });
});
