import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, waitFor } from "@testing-library/react";

const { mockPush } = vi.hoisted(() => ({
  mockPush: vi.fn(),
}));

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: mockPush }),
  useSearchParams: () => new URLSearchParams(),
}));

vi.mock("@multica/core/paths", () => ({
  paths: { login: () => "/login" },
}));

import CallbackPage from "./page";

describe("CallbackPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("redirects to /login", async () => {
    render(<CallbackPage />);
    await waitFor(() => {
      expect(mockPush).toHaveBeenCalledWith("/login");
    });
  });
});
