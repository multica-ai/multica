// @vitest-environment jsdom

import { describe, it, expect, vi } from "vitest";
import { render } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enRuntimes from "../../locales/en/runtimes.json";
import { RuntimeDetailPage } from "./runtime-detail-page";

// MUL-7107: the loading header used to sit on a bare gutter while the header
// that replaced it rode the rail, so on a wide window the title jumped inward
// the moment the query resolved. The skeleton is not exported, so drive it
// through the page's own isLoading branch rather than reaching past the seam.
//
// PAGE_RAIL is overridden with a sentinel because its real value is an
// ordinary Tailwind class that a hand-written element could match by accident.
const RAIL_SENTINEL = "rail-sentinel";
vi.mock("../../layout/page-header", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../../layout/page-header")>()),
  PAGE_RAIL: "rail-sentinel",
}));

vi.mock("@tanstack/react-query", async () => {
  const actual =
    await vi.importActual<typeof import("@tanstack/react-query")>(
      "@tanstack/react-query",
    );
  return {
    ...actual,
    useQuery: () => ({ data: [], isLoading: true }),
    useQueryClient: () => ({ invalidateQueries: vi.fn() }),
  };
});
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (sel: (s: { user: { id: string } }) => unknown) =>
    sel({ user: { id: "user-1" } }),
}));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ runtimes: () => "/runtimes", agentDetail: () => "/agents" }),
}));
vi.mock("@multica/core/realtime", () => ({ useWSEvent: () => {} }));
vi.mock("@multica/core/agents", () => ({
  agentTaskSnapshotOptions: () => ({ queryKey: ["tasks"] }),
}));
vi.mock("@multica/core/runtimes", () => ({
  runtimeProfileListOptions: () => ({ queryKey: ["profiles"] }),
  deriveRuntimeHealth: () => "online",
  runtimeDisplayName: () => "machine",
  isRuntimeUsableForUser: () => true,
  parseRuntimeProfileBoundConflict: () => null,
  useDeleteRuntimeProfile: () => ({ mutateAsync: vi.fn(), isPending: false }),
}));
vi.mock("@multica/core/runtimes/queries", () => ({
  runtimeListOptions: () => ({ queryKey: ["runtimes"] }),
  runtimeKeys: { list: () => ["runtimes"] },
}));
vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: () => ({ queryKey: ["agents"] }),
  memberListOptions: () => ({ queryKey: ["members"] }),
}));

describe("RuntimeDetailPage loading state", () => {
  it("puts the loading header on the same rail as the loaded header", () => {
    const { container } = render(
      <I18nProvider
        locale="en"
        resources={{ en: { common: enCommon, runtimes: enRuntimes } }}
      >
        <RuntimeDetailPage runtimeId="rt-1" />
      </I18nProvider>,
    );

    const railed = container.querySelectorAll(`.${RAIL_SENTINEL}`);
    // Header band and body band; neither may be left on a bare gutter, or the
    // page shifts sideways when loading finishes.
    expect(railed.length).toBeGreaterThanOrEqual(2);
    expect(container.querySelector(".border-b")).toContainElement(
      container.querySelector(`.${RAIL_SENTINEL}`) as HTMLElement,
    );
  });
});
