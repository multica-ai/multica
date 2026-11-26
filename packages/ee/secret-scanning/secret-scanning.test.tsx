import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import { getSecretScanningExtension } from "@multica/views/extensions";
import { renderWithI18n } from "../test/i18n";
import { registerEE } from "../index";
import { secretScanningExtension } from "./components";
import { secretScanApi } from "./data";

const member = vi.hoisted(() => ({ role: "member" }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws" }));
vi.mock("@multica/core/realtime", () => ({ useWSEvent: () => {}, useWSReconnect: () => {} }));
vi.mock("@multica/core/permissions", () => ({
  useCurrentMember: () => ({ role: member.role }),
  useIsGuest: () => false,
}));
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: (_type: string, id: string) => (id === "u1" ? "Ada" : "Grace") }),
}));
vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<object>()),
  useWorkspacePaths: () => ({ issueDetail: (ref: string) => `/acme/issues/${ref}` }),
  useCurrentWorkspace: () => ({ id: "ws", name: "Acme" }),
}));
vi.mock("@multica/views/navigation", async (importOriginal) => ({
  ...(await importOriginal<object>()),
  useNavigation: () => ({ push: vi.fn() }),
}));

const { CommentNotice, settings } = secretScanningExtension;

function finding(over: Record<string, unknown> = {}) {
  return {
    id: "f1",
    issue_id: "i1",
    issue_identifier: "DEV-3",
    issue_title: "Payments webhook 502",
    comment_id: "c1",
    author_type: "member",
    author_id: "u1",
    rule: "github_token",
    masked: "ghp_••••••••0oQi",
    fingerprint: "fp1",
    status: "open",
    resolved_by_type: null,
    resolved_by_id: null,
    resolved_at: null,
    created_at: new Date().toISOString(),
    ...over,
  };
}

let qc: QueryClient;
let request: ReturnType<typeof vi.fn>;

beforeEach(() => {
  member.role = "member";
  qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  request = vi.fn().mockImplementation(async (path: string) => {
    if (path === "/api/secret-scan/issues/i1/findings") {
      return { findings: [finding(), finding({ id: "f2", comment_id: "c2", rule: "openai_api_key" })] };
    }
    if (path === "/api/secret-scan/settings") return { mode: "notify", backfill: { status: "idle", scanned: 0 } };
    if (path === "/api/secret-scan/findings?status=open") {
      return {
        findings: [finding(), finding({ id: "f3", comment_id: "c3", author_id: "u2" }), finding({ id: "f4", fingerprint: "fp2", rule: "aws_access_key", masked: "AKIA••••••••MPLE" })],
        counts: { open: 3, redacted: 12, dismissed: 2 },
      };
    }
    return undefined;
  });
  setApiInstance({ request } as unknown as ApiClient);
});
afterEach(() => qc.clear());

function wrap(ui: ReactNode) {
  return renderWithI18n(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

it("registers with the app", () => {
  registerEE();
  expect(getSecretScanningExtension()).toBe(secretScanningExtension);
});

it("drops findings it cannot read and keeps the rest", async () => {
  request.mockResolvedValueOnce({ findings: [finding(), { id: 7 }, null] });
  expect((await secretScanApi.issueFindings("i1")).map((f) => f.id)).toEqual(["f1"]);
});

it("falls back to the defaults for malformed settings", async () => {
  request.mockResolvedValueOnce({ mode: "shred", backfill: "soon" });
  expect(await secretScanApi.settings()).toEqual({
    mode: "notify",
    backfill: { status: "idle", scanned: 0, started_at: null, finished_at: null },
  });
});

it("shows a comment's findings under it and redacts them", async () => {
  wrap(<CommentNotice issueId="i1" commentId="c1" />);
  expect(await screen.findByText("Possible GitHub token in this comment")).toBeInTheDocument();

  await userEvent.click(screen.getByRole("button", { name: "Redact" }));
  await waitFor(() =>
    expect(request).toHaveBeenCalledWith("/api/secret-scan/findings/redact", {
      method: "POST",
      body: JSON.stringify({ finding_ids: ["f1"] }),
    }),
  );
});

it("dismisses a finding that is not a secret", async () => {
  wrap(<CommentNotice issueId="i1" commentId="c2" />);
  await userEvent.click(await screen.findByRole("button", { name: "Not a secret" }));
  await waitFor(() =>
    expect(request).toHaveBeenCalledWith("/api/secret-scan/findings/dismiss", {
      method: "POST",
      body: JSON.stringify({ finding_ids: ["f2"] }),
    }),
  );
});

it("shows nothing under a comment without findings", async () => {
  const { container } = wrap(<CommentNotice issueId="i1" commentId="c9" />);
  await waitFor(() => expect(request).toHaveBeenCalled());
  expect(container).toBeEmptyDOMElement();
});

it("does not load the settings for a member", async () => {
  wrap(<settings.Content />);
  expect(await screen.findByText("Security")).toBeInTheDocument();
  expect(request).not.toHaveBeenCalledWith("/api/secret-scan/settings");
  expect(screen.queryByText("Findings")).not.toBeInTheDocument();
});

it("lists one row per secret for an admin and redacts every comment it is in", async () => {
  member.role = "admin";
  wrap(<settings.Content />);
  expect(await screen.findByText("In 2 comments")).toBeInTheDocument();
  expect(screen.getByText("AKIA••••••••MPLE")).toBeInTheDocument();
  expect(screen.getByText(/Ada's comment/)).toBeInTheDocument();

  await userEvent.click(screen.getAllByRole("button", { name: "Redact" })[0]!);
  await waitFor(() =>
    expect(request).toHaveBeenCalledWith("/api/secret-scan/findings/redact", {
      method: "POST",
      body: JSON.stringify({ finding_ids: ["f1", "f3"] }),
    }),
  );
});
