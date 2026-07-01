import { render } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const { mockRuntimesPage } = vi.hoisted(() => ({
  mockRuntimesPage: vi.fn(
    (_props: { visibleProviders?: readonly string[] }) => null,
  ),
}));

vi.mock("@multica/views/runtimes", () => ({
  RuntimesPage: mockRuntimesPage,
}));

import RuntimesRoute from "./page";

describe("RuntimesRoute", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("limits the web runtime view to CSC", () => {
    render(<RuntimesRoute />);

    expect(mockRuntimesPage.mock.calls[0]?.[0]).toEqual(
      expect.objectContaining({ visibleProviders: ["csc"] }),
    );
  });
});
