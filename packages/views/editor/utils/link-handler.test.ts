import { afterEach, describe, expect, it, vi } from "vitest";
import { openLink } from "./link-handler";

function lastNavigatedPath(): string | undefined {
  const event = vi.mocked(window.dispatchEvent).mock.calls.at(-1)?.[0];
  return event instanceof CustomEvent
    ? (event.detail as { path?: string }).path
    : undefined;
}

describe("openLink", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("prepends the current workspace slug to legacy squad links", () => {
    vi.spyOn(window, "dispatchEvent");

    openLink("/squads/abc", "acme");

    expect(lastNavigatedPath()).toBe("/acme/squads/abc");
  });

  it("does not prepend the workspace slug to already scoped squad links", () => {
    vi.spyOn(window, "dispatchEvent");

    openLink("/acme/squads/abc", "acme");

    expect(lastNavigatedPath()).toBe("/acme/squads/abc");
  });

  it("does not prepend the workspace slug to global paths", () => {
    vi.spyOn(window, "dispatchEvent");

    openLink("/login", "acme");

    expect(lastNavigatedPath()).toBe("/login");
  });
});
