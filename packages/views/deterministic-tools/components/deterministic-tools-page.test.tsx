// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";

const mockTest = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", () => ({
  api: { testDeterministicTool: (...args: unknown[]) => mockTest(...args) },
}));

import { DeterministicToolsPage } from "./deterministic-tools-page";

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <DeterministicToolsPage />
    </QueryClientProvider>,
  );
}

beforeEach(() => mockTest.mockReset());

describe("DeterministicToolsPage", () => {
  it("runs the step and renders the returned Result envelope", async () => {
    mockTest.mockResolvedValue({
      status: "ok",
      summary: "Greeted world",
      machine_data: { length: 5 },
      retryable: false,
    });

    renderPage();
    fireEvent.click(screen.getByRole("button", { name: /test/i }));

    await waitFor(() => screen.getByText("Greeted world"));
    expect(mockTest).toHaveBeenCalledWith(
      expect.objectContaining({ source: expect.stringContaining("package step") }),
    );
  });

  it("rejects invalid sample input without hitting the API", async () => {
    renderPage();

    // Two textareas: [0] source, [1] sample input.
    const inputArea = screen.getAllByRole("textbox")[1]!;
    fireEvent.change(inputArea, { target: { value: "{ not json" } });
    fireEvent.click(screen.getByRole("button", { name: /test/i }));

    await waitFor(() => screen.getByText(/valid JSON/i));
    expect(mockTest).not.toHaveBeenCalled();
  });
});
