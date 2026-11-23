import { useContext } from "react";
import { act, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { NavigationProvider, useNavigation } from "./context";
import type { NavigationAdapter } from "./types";
import { ReviewSessionContext, type ReviewSession } from "../issues/review/session";

describe("review navigation ownership", () => {
  it("preserves an owned transition across stale renders, expires manual navigation and isolates logical tabs", () => {
    let session: ReviewSession;
    let nav: NavigationAdapter;
    function Probe() {
      session = useContext(ReviewSessionContext)!;
      nav = useNavigation();
      return <button onClick={() => nav.push("/next")}>Manual navigation</button>;
    }
    const adapter = (pathname: string, sessionKey = "tab-a"): NavigationAdapter => ({
      pathname, sessionKey, searchParams: new URLSearchParams(), hash: "",
      push: vi.fn(), replace: vi.fn(), back: vi.fn(), getShareableUrl: (p) => p,
    });
    const { rerender } = render(<NavigationProvider value={adapter("/list")}><Probe /></NavigationProvider>);
    const first = session!;
    const receipt = first.receipt;
    first.preserveNavigation = "/next";
    act(() => nav!.replace("/next"));
    const lease = first.epoch;
    expect(first.receipt).toBe(receipt);
    rerender(<NavigationProvider value={adapter("/list")}><Probe /></NavigationProvider>);
    expect(first.epoch).toBe(lease);
    rerender(<NavigationProvider value={adapter("/next")}><Probe /></NavigationProvider>);
    expect(first.epoch).toBe(lease);
    act(() => screen.getByText("Manual navigation").click());
    expect(first.receipt).toBeGreaterThan(receipt);
    rerender(<NavigationProvider value={adapter("/next", "tab-b")}><Probe /></NavigationProvider>);
    expect(session!).not.toBe(first);
    expect(session!.origin).toBeNull();
    expect(first.origin).toBeNull();
  });
});
