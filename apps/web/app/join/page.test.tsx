import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, waitFor } from "@testing-library/react";
import { documentNavigation } from "@/platform/web-host-path";
import JoinPage from "./page";

const searchParamsState = vi.hoisted(() => ({
  params: new URLSearchParams(),
}));

vi.mock("next/navigation", () => ({
  useSearchParams: () => searchParamsState.params,
}));

describe("JoinPage", () => {
  beforeEach(() => {
    searchParamsState.params = new URLSearchParams();
    vi.spyOn(documentNavigation, "replace").mockImplementation(() => {});
  });

  it("hands a legacy share-link code to the VIBES-owned Tag journey", async () => {
    searchParamsState.params = new URLSearchParams({ code: "abc123" });

    render(<JoinPage />);

    await waitFor(() =>
      expect(documentNavigation.replace).toHaveBeenCalledWith(
        "/tag/join?token=abc123",
      ),
    );
  });

  it("preserves a canonical VIBES token", async () => {
    searchParamsState.params = new URLSearchParams({ token: "vibes token" });

    render(<JoinPage />);

    await waitFor(() =>
      expect(documentNavigation.replace).toHaveBeenCalledWith(
        "/tag/join?token=vibes%20token",
      ),
    );
  });
});
