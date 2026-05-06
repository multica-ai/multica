import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

// ---------------------------------------------------------------------------
// Hoisted mocks
// ---------------------------------------------------------------------------

const mockUseQueryClient = vi.hoisted(() =>
  vi.fn(() => ({
    getQueryData: vi.fn(),
    setQueryData: vi.fn(),
  })),
);
const mockUseCurrentWorkspace = vi.hoisted(() => vi.fn());
const mockToastSuccess = vi.hoisted(() => vi.fn());
const mockToastError = vi.hoisted(() => vi.fn());

vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => mockUseQueryClient(),
}));

vi.mock("@multica/core/api", () => ({
  api: { updateWorkspace: vi.fn() },
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => mockUseCurrentWorkspace(),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  workspaceKeys: { list: () => ["workspaces", "list"] },
}));

vi.mock("sonner", () => ({
  toast: {
    success: (msg: string) => mockToastSuccess(msg),
    error: (msg: string) => mockToastError(msg),
  },
}));

// ---------------------------------------------------------------------------
// Imports after mocks
// ---------------------------------------------------------------------------

import {
  ProductCapabilitiesProvider,
  type ProductCapabilities,
} from "@multica/core/platform";
import { LOCAL_PRODUCT_CAPABILITIES } from "@multica/core/config";
import { LabsTab } from "./labs-tab";

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

const baseWorkspace = {
  id: "ws-1",
  slug: "acme",
  name: "Acme",
  description: "",
  context: "",
  settings: {},
};

const sampleDiagnostics = {
  appVersion: "1.2.3",
  apiUrl: "http://localhost:8080",
  os: "macos" as const,
  paths: {
    root: "/Users/me/Library/Application Support/Multica",
    postgresData:
      "/Users/me/Library/Application Support/Multica/postgres/data",
    postgresLogs:
      "/Users/me/Library/Application Support/Multica/postgres/logs",
    daemonLogs:
      "/Users/me/Library/Application Support/Multica/daemon/logs",
    appLogs: "/Users/me/Library/Logs/Multica",
    appConfig: "/Users/me/Library/Application Support/Multica",
  },
  stack: {
    overall: "ready" as const,
    components: [
      {
        name: "database" as const,
        state: "ready" as const,
        detail: null,
        updatedAt: 1,
      },
    ],
  },
  daemonVersion: "0.1.42",
  collectedAt: "2026-05-05T10:11:12.000Z",
};

function setupDiagnosticsAPI(overrides: Partial<{
  get: ReturnType<typeof vi.fn>;
  formatAsText: ReturnType<typeof vi.fn>;
  openPath: ReturnType<typeof vi.fn>;
}> = {}): {
  get: ReturnType<typeof vi.fn>;
  formatAsText: ReturnType<typeof vi.fn>;
  openPath: ReturnType<typeof vi.fn>;
} {
  const api = {
    get: overrides.get ?? vi.fn().mockResolvedValue(sampleDiagnostics),
    formatAsText:
      overrides.formatAsText ??
      vi.fn().mockResolvedValue("Multica local diagnostics\n…"),
    openPath:
      overrides.openPath ?? vi.fn().mockResolvedValue({ ok: true }),
  };
  Object.defineProperty(window, "localDiagnosticsAPI", {
    configurable: true,
    value: api,
  });
  return api;
}

// userEvent.setup() installs its own navigator.clipboard. Spy on writeText
// so we intercept the call without clobbering userEvent's internals — this is
// the same pattern search-command.test.tsx uses.
function spyOnClipboardWrite(): ReturnType<typeof vi.spyOn> {
  return vi
    .spyOn(navigator.clipboard, "writeText")
    .mockImplementation(() => Promise.resolve());
}

const localCapabilities: ProductCapabilities = LOCAL_PRODUCT_CAPABILITIES;
const cloudLikeCapabilities: ProductCapabilities = {
  ...LOCAL_PRODUCT_CAPABILITIES,
  settings: {
    ...LOCAL_PRODUCT_CAPABILITIES.settings,
    showDiagnostics: false,
  },
};

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("LabsTab — Local diagnostics section", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockUseCurrentWorkspace.mockReturnValue(baseWorkspace);
  });

  it("renders one entry per local data path under local capabilities", async () => {
    setupDiagnosticsAPI();

    render(
      <ProductCapabilitiesProvider capabilities={localCapabilities}>
        <LabsTab />
      </ProductCapabilitiesProvider>,
    );

    // Wait for the section to render once paths arrive.
    await screen.findByText(sampleDiagnostics.paths.postgresData);

    // Every key gets its own row — one Open button per row.
    const openButtons = screen.getAllByRole("button", { name: /open/i });
    expect(openButtons).toHaveLength(
      Object.keys(sampleDiagnostics.paths).length,
    );

    // Every distinct path value is reachable in the DOM (root and appConfig
    // legitimately share the same path on most platforms — we just need at
    // least one match per distinct value).
    const distinctValues = Array.from(
      new Set(Object.values(sampleDiagnostics.paths)),
    );
    for (const value of distinctValues) {
      expect(screen.getAllByText(value).length).toBeGreaterThan(0);
    }
  });

  it("does not call the diagnostics bridge when capabilities hide the section", async () => {
    const api = setupDiagnosticsAPI();

    render(
      <ProductCapabilitiesProvider capabilities={cloudLikeCapabilities}>
        <LabsTab />
      </ProductCapabilitiesProvider>,
    );

    // Give any (incorrect) effect a tick to fire — none should.
    await Promise.resolve();
    expect(api.get).not.toHaveBeenCalled();
    expect(screen.queryByText(/local diagnostics/i)).toBeNull();
  });

  it("invokes openPath with the matching path key when a folder button is clicked", async () => {
    const api = setupDiagnosticsAPI();
    const user = userEvent.setup();

    render(
      <ProductCapabilitiesProvider capabilities={localCapabilities}>
        <LabsTab />
      </ProductCapabilitiesProvider>,
    );

    // Wait for diagnostics to arrive and the postgresData row to render.
    await screen.findByText(sampleDiagnostics.paths.postgresData);

    const openButtons = screen.getAllByRole("button", { name: /open/i });
    // postgresData is the second key in the LocalDataPaths shape (root,
    // postgresData, postgresLogs, daemonLogs, appLogs, appConfig). The order
    // of buttons mirrors the order of paths.
    const postgresDataButton = openButtons[1];
    if (!postgresDataButton) throw new Error("expected an Open button for postgresData");
    await user.click(postgresDataButton);

    expect(api.openPath).toHaveBeenCalledTimes(1);
    expect(api.openPath).toHaveBeenCalledWith("postgresData");
  });

  it("copies formatted diagnostics to the clipboard via the bridge", async () => {
    const api = setupDiagnosticsAPI();
    const user = userEvent.setup();
    const writeText = spyOnClipboardWrite();

    render(
      <ProductCapabilitiesProvider capabilities={localCapabilities}>
        <LabsTab />
      </ProductCapabilitiesProvider>,
    );

    const copyButton = await screen.findByRole("button", {
      name: /copy diagnostics/i,
    });
    await user.click(copyButton);

    await waitFor(() => {
      expect(api.formatAsText).toHaveBeenCalledTimes(1);
    });
    await waitFor(() => {
      expect(writeText).toHaveBeenCalledWith("Multica local diagnostics\n…");
    });
    expect(mockToastSuccess).toHaveBeenCalled();
  });
});
