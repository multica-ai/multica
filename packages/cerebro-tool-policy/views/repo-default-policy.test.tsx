import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";

const mockCerebroRequest = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/api")>("@multica/core/api");
  return { ...actual, api: { cerebroRequest: mockCerebroRequest } };
});

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

// shadcn Select renders through a Base UI portal; flatten it to a native <select>
// so the value/change behaviour is drivable in jsdom.
vi.mock("@multica/ui/components/ui/select", () => ({
  Select: ({
    value,
    onValueChange,
    disabled,
    children,
  }: {
    value: string;
    onValueChange: (v: string) => void;
    disabled?: boolean;
    children: ReactNode;
  }) => (
    <select
      aria-label="Default repo access"
      value={value}
      disabled={disabled}
      onChange={(e) => onValueChange(e.target.value)}
    >
      {children}
    </select>
  ),
  SelectTrigger: () => null,
  SelectValue: () => null,
  SelectContent: ({ children }: { children: ReactNode }) => <>{children}</>,
  SelectItem: ({ value, children }: { value: string; children: ReactNode }) => (
    <option value={value}>{children}</option>
  ),
}));

import { RepoDefaultPolicy } from "./repo-default-policy";

const REPO = "github.com/firtal-group/repo-a";

function renderControl() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <RepoDefaultPolicy wsId="ws-1" repoUrl={REPO} />
    </QueryClientProvider>,
  );
}

function callsByMethod(method: string | undefined) {
  return mockCerebroRequest.mock.calls.filter(
    (c) => (c[1] as RequestInit | undefined)?.method === method,
  );
}

beforeEach(() => {
  mockCerebroRequest.mockReset();
  // GET table: no existing repo rows → current default is "inherit".
  mockCerebroRequest.mockResolvedValue({ tools: [] });
});

describe("RepoDefaultPolicy", () => {
  it("writes the chosen default to all three repo capabilities at the workspace layer, scoped to the repo", async () => {
    const user = userEvent.setup();
    renderControl();
    const select = await screen.findByLabelText("Default repo access");
    await user.selectOptions(select, "deny");

    await waitFor(() => {
      const puts = callsByMethod("PUT").map((c) => JSON.parse((c[1] as RequestInit).body as string));
      const denyForRepo = puts.filter((b) => b.resource_pattern === REPO && b.setting === "deny");
      expect(new Set(denyForRepo.map((b) => b.tool_key))).toEqual(
        new Set(["repo.read", "repo.checkout", "repo.push"]),
      );
      for (const b of denyForRepo) {
        expect(b).toMatchObject({ layer: "workspace", subject_id: "ws-1" });
      }
    });
  });

  it("'No default' clears the workspace layer for all three capabilities", async () => {
    const user = userEvent.setup();
    renderControl();
    const select = await screen.findByLabelText("Default repo access");
    await user.selectOptions(select, "inherit");

    await waitFor(() => {
      const deletes = callsByMethod("DELETE").map((c) => String(c[0]));
      const forRepo = deletes.filter((u) => u.includes(encodeURIComponent(REPO)) && u.includes("layer=workspace"));
      expect(forRepo.length).toBe(3);
    });
  });
});
