// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";

const mockTest = vi.hoisted(() => vi.fn());
const mockList = vi.hoisted(() => vi.fn());
const mockCreate = vi.hoisted(() => vi.fn());
const mockUpdate = vi.hoisted(() => vi.fn());
const mockDelete = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", () => ({
  api: {
    testDeterministicTool: (...args: unknown[]) => mockTest(...args),
    listDeterministicTools: (...args: unknown[]) => mockList(...args),
    createDeterministicTool: (...args: unknown[]) => mockCreate(...args),
    updateDeterministicTool: (...args: unknown[]) => mockUpdate(...args),
    deleteDeterministicTool: (...args: unknown[]) => mockDelete(...args),
  },
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

import { DeterministicToolsPage } from "./deterministic-tools-page";

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <DeterministicToolsPage />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  mockTest.mockReset();
  mockList.mockReset().mockResolvedValue([]);
  mockCreate.mockReset();
  mockUpdate.mockReset();
  mockDelete.mockReset();
});

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

  it("saves a new tool via createDeterministicTool", async () => {
    mockCreate.mockResolvedValue({
      id: "t-1",
      workspace_id: "ws-1",
      name: "greet",
      description: "",
      source: "package step",
      enabled: true,
      created_at: "",
      updated_at: "",
    });

    renderPage();
    // Name field is the first textbox; set a valid name, then Save.
    const nameInput = screen.getByPlaceholderText(/snake_case/i);
    fireEvent.change(nameInput, { target: { value: "greet" } });
    fireEvent.click(screen.getByRole("button", { name: /save/i }));

    await waitFor(() => expect(mockCreate).toHaveBeenCalled());
    expect(mockCreate).toHaveBeenCalledWith(
      expect.objectContaining({ name: "greet", enabled: true }),
    );
  });

  it("rejects invalid sample input without hitting the API", async () => {
    renderPage();
    const inputArea = screen
      .getAllByRole("textbox")
      .find((el) => (el as HTMLTextAreaElement).value.includes("world"))!;
    fireEvent.change(inputArea, { target: { value: "{ not json" } });
    fireEvent.click(screen.getByRole("button", { name: /test/i }));

    await waitFor(() => screen.getByText(/valid JSON/i));
    expect(mockTest).not.toHaveBeenCalled();
  });
});
