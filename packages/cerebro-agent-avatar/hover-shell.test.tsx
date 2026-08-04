// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, fireEvent, cleanup } from "@testing-library/react";
import { ActorAvatarHoverCardShell } from "./hover-shell";

const coarse = { current: false };
const mobile = { current: false };

vi.mock("./use-coarse-pointer", () => ({
  useIsCoarsePointer: () => coarse.current,
  readCoarsePointer: () => coarse.current,
}));

vi.mock("@multica/ui/hooks/use-mobile", () => ({
  useIsMobile: () => mobile.current,
}));

describe("ActorAvatarHoverCardShell", () => {
  beforeEach(() => {
    coarse.current = false;
    mobile.current = false;
  });
  afterEach(() => {
    cleanup();
    document.body.innerHTML = "";
  });

  it("fine pointer uses hover-card trigger (no touch trigger)", () => {
    render(
      <ActorAvatarHoverCardShell content={<div>card-body</div>}>
        <span>avatar</span>
      </ActorAvatarHoverCardShell>,
    );
    expect(document.querySelector('[data-slot="actor-avatar-touch-trigger"]')).toBeNull();
    expect(document.querySelector('[data-slot="hover-card-trigger"]')).toBeTruthy();
  });

  it("coarse + narrow opens bottom sheet on tap", () => {
    coarse.current = true;
    mobile.current = true;
    render(
      <ActorAvatarHoverCardShell content={<div>card-body</div>}>
        <span>avatar</span>
      </ActorAvatarHoverCardShell>,
    );
    const trigger = document.querySelector(
      '[data-slot="actor-avatar-touch-trigger"]',
    );
    expect(trigger).toBeTruthy();
    fireEvent.click(trigger!);
    expect(screen.getByText("card-body")).toBeTruthy();
    expect(document.querySelector('[data-slot="actor-avatar-touch-sheet"]')).toBeTruthy();
  });

  it("coarse + wide opens popover on tap", () => {
    coarse.current = true;
    mobile.current = false;
    render(
      <ActorAvatarHoverCardShell content={<div>card-body</div>}>
        <span>avatar</span>
      </ActorAvatarHoverCardShell>,
    );
    const trigger = document.querySelector(
      '[data-slot="actor-avatar-touch-trigger"]',
    );
    expect(trigger).toBeTruthy();
    fireEvent.click(trigger!);
    expect(screen.getByText("card-body")).toBeTruthy();
  });
});
