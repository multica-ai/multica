// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { useSessionTabsEnabled } from "./session-tabs";

vi.mock("@multica/ui/hooks/use-mobile", () => ({
  useIsCompact: () => mock.compact,
}));

const mock = vi.hoisted(() => ({ compact: false }));

describe("useSessionTabsEnabled", () => {
  it("turns on after mount when the viewport is not compact", async () => {
    mock.compact = false;
    const { result } = renderHook(() => useSessionTabsEnabled());
    await waitFor(() => expect(result.current).toBe(true));
  });

  it("stays off on compact after mount", async () => {
    mock.compact = true;
    const { result } = renderHook(() => useSessionTabsEnabled());
    await waitFor(() => expect(result.current).toBe(false));
  });
});
