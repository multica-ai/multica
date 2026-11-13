// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import type { AgentRuntime } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enRuntimes from "../../locales/en/runtimes.json";
import {
  RuntimeConcurrencyField,
  parseRuntimeConcurrencyDraft,
} from "./runtime-concurrency-field";

const mockUpdateRuntime = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/api", async (importOriginal) => ({
  ...await importOriginal<typeof import("@multica/core/api")>(),
  api: {
    updateRuntime: (...args: unknown[]) => mockUpdateRuntime(...args),
  },
}));

vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

function makeRuntime(max: number | null): AgentRuntime {
  return {
    id: "rt-1",
    workspace_id: "ws-1",
    daemon_id: null,
    name: "Claude",
    runtime_mode: "local",
    provider: "claude",
    launch_header: "",
    status: "online",
    device_info: "",
    metadata: {},
    owner_id: "user-me",
    visibility: "private",
    shared_with: [],
    max_concurrent_tasks: max,
    last_seen_at: null,
    created_at: "",
    updated_at: "",
  };
}

function renderField(runtime: AgentRuntime, canEdit = true) {
  const qc = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  return render(
    <I18nProvider locale="en" resources={{ en: { runtimes: enRuntimes } }}>
      <QueryClientProvider client={qc}>
        <RuntimeConcurrencyField runtime={runtime} canEdit={canEdit} />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

describe("parseRuntimeConcurrencyDraft", () => {
  it("maps empty to no limit and validates the range", () => {
    expect(parseRuntimeConcurrencyDraft("")).toBeNull();
    expect(parseRuntimeConcurrencyDraft("  ")).toBeNull();
    expect(parseRuntimeConcurrencyDraft("1")).toBe(1);
    expect(parseRuntimeConcurrencyDraft("50")).toBe(50);
    expect(parseRuntimeConcurrencyDraft("0")).toBeUndefined();
    expect(parseRuntimeConcurrencyDraft("51")).toBeUndefined();
    expect(parseRuntimeConcurrencyDraft("1.5")).toBeUndefined();
  });
});

describe("RuntimeConcurrencyField", () => {
  beforeEach(() => {
    mockUpdateRuntime.mockReset();
    mockUpdateRuntime.mockResolvedValue(makeRuntime(1));
  });

  it("saves a new limit on Enter", async () => {
    renderField(makeRuntime(null));
    const input = screen.getByLabelText("Concurrency");
    fireEvent.change(input, { target: { value: "1" } });
    fireEvent.keyDown(input, { key: "Enter" });
    await waitFor(() =>
      expect(mockUpdateRuntime).toHaveBeenCalledWith("rt-1", { max_concurrent_tasks: 1 }),
    );
  });

  it("clears the limit when emptied", async () => {
    renderField(makeRuntime(3));
    const input = screen.getByLabelText("Concurrency");
    fireEvent.change(input, { target: { value: "" } });
    fireEvent.blur(input);
    await waitFor(() =>
      expect(mockUpdateRuntime).toHaveBeenCalledWith("rt-1", { max_concurrent_tasks: null }),
    );
  });

  it("reverts an out-of-range value without saving", () => {
    renderField(makeRuntime(3));
    const input = screen.getByLabelText("Concurrency") as HTMLInputElement;
    fireEvent.change(input, { target: { value: "99" } });
    fireEvent.blur(input);
    expect(mockUpdateRuntime).not.toHaveBeenCalled();
    expect(input.value).toBe("3");
  });

  it("is read-only without edit permission", () => {
    renderField(makeRuntime(null), false);
    expect(screen.queryByLabelText("Concurrency")).toBeNull();
    expect(screen.getByText("No limit")).toBeTruthy();
  });
});
