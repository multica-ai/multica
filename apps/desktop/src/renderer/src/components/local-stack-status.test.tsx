import { describe, expect, it, vi, beforeEach } from "vitest";
import { act, fireEvent, render, screen } from "@testing-library/react";

import type {
  LocalStackStatus,
  LocalStackComponentStatus,
} from "../../../shared/local-stack-types";
import { LocalStackStatusScreen, useLocalStackStatus } from "./local-stack-status";

type StatusListener = (s: LocalStackStatus) => void;

function buildStatus(
  overall: LocalStackStatus["overall"],
  overrides: Partial<Record<LocalStackComponentStatus["name"], LocalStackComponentStatus>> = {},
): LocalStackStatus {
  const now = Date.now();
  const defaults: LocalStackComponentStatus[] = [
    { name: "database", state: "pending", detail: null, updatedAt: now },
    { name: "migrations", state: "pending", detail: null, updatedAt: now },
    { name: "api", state: "pending", detail: null, updatedAt: now },
    { name: "bootstrap", state: "pending", detail: null, updatedAt: now },
    { name: "daemon", state: "pending", detail: null, updatedAt: now },
    { name: "runtimeRegistration", state: "pending", detail: null, updatedAt: now },
  ];
  return {
    overall,
    components: defaults.map((c) => overrides[c.name] ?? c),
  };
}

function mockLocalStackAPI(initial: LocalStackStatus): {
  retry: ReturnType<typeof vi.fn>;
  openLogs: ReturnType<typeof vi.fn>;
  getStatus: ReturnType<typeof vi.fn>;
  emit: (s: LocalStackStatus) => void;
} {
  const listeners: StatusListener[] = [];
  const api = {
    getStatus: vi.fn(async () => initial),
    retry: vi.fn(async () => {}),
    openLogs: vi.fn(async () => ({ ok: true })),
    onStatusChange: vi.fn((cb: StatusListener) => {
      listeners.push(cb);
      return () => {
        const i = listeners.indexOf(cb);
        if (i >= 0) listeners.splice(i, 1);
      };
    }),
  };
  (window as unknown as { localStackAPI: typeof api }).localStackAPI = api;
  return {
    retry: api.retry,
    openLogs: api.openLogs,
    getStatus: api.getStatus,
    emit: (s) => listeners.forEach((cb) => cb(s)),
  };
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe("LocalStackStatusScreen", () => {
  it("renders all six components in canonical order with their detail strings", () => {
    mockLocalStackAPI(buildStatus("failing"));
    const status = buildStatus("failing", {
      database: {
        name: "database",
        state: "ready",
        detail: null,
        updatedAt: 1,
      },
      migrations: {
        name: "migrations",
        state: "failing",
        detail: "schema lock conflict",
        updatedAt: 2,
      },
    });

    render(<LocalStackStatusScreen status={status} />);

    expect(screen.getByText("Database")).toBeInTheDocument();
    expect(screen.getByText("Migrations")).toBeInTheDocument();
    expect(screen.getByText("API")).toBeInTheDocument();
    expect(screen.getByText("Bootstrap")).toBeInTheDocument();
    expect(screen.getByText("Daemon")).toBeInTheDocument();
    expect(screen.getByText("Runtime registration")).toBeInTheDocument();
    expect(screen.getByText("schema lock conflict")).toBeInTheDocument();
  });

  it("retry button calls window.localStackAPI.retry once", () => {
    const api = mockLocalStackAPI(buildStatus("failing"));
    render(<LocalStackStatusScreen status={buildStatus("failing")} />);
    fireEvent.click(screen.getByRole("button", { name: /retry/i }));
    expect(api.retry).toHaveBeenCalledTimes(1);
  });

  it("retry button is disabled while overall is starting and enabled when failing", () => {
    mockLocalStackAPI(buildStatus("starting"));
    const { rerender } = render(
      <LocalStackStatusScreen
        status={buildStatus("starting", {
          database: {
            name: "database",
            state: "starting",
            detail: null,
            updatedAt: 1,
          },
        })}
      />,
    );
    expect(screen.getByRole("button", { name: /retry/i })).toBeDisabled();

    rerender(<LocalStackStatusScreen status={buildStatus("failing")} />);
    expect(screen.getByRole("button", { name: /retry/i })).not.toBeDisabled();
  });

  it("open logs button calls window.localStackAPI.openLogs once", () => {
    const api = mockLocalStackAPI(buildStatus("failing"));
    render(<LocalStackStatusScreen status={buildStatus("failing")} />);
    fireEvent.click(screen.getByRole("button", { name: /open logs/i }));
    expect(api.openLogs).toHaveBeenCalledTimes(1);
  });
});

describe("useLocalStackStatus", () => {
  function HookProbe() {
    const status = useLocalStackStatus();
    if (!status) return <span>loading</span>;
    return <span>overall:{status.overall}</span>;
  }

  it("returns null until first IPC, then resolves with status", async () => {
    mockLocalStackAPI(buildStatus("starting"));
    render(<HookProbe />);
    expect(screen.getByText("loading")).toBeInTheDocument();
    expect(await screen.findByText("overall:starting")).toBeInTheDocument();
  });

  it("re-renders when onStatusChange listener fires", async () => {
    const api = mockLocalStackAPI(buildStatus("starting"));
    render(<HookProbe />);
    expect(await screen.findByText("overall:starting")).toBeInTheDocument();
    act(() => {
      api.emit(buildStatus("ready"));
    });
    expect(await screen.findByText("overall:ready")).toBeInTheDocument();
  });
});
