import { beforeEach, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithI18n } from "../../test/i18n";
import type { GongfengRepository, VCSConnection } from "@multica/core/types";

const mocks = vi.hoisted(() => ({ enable: vi.fn(), disable: vi.fn(), invalidate: vi.fn(), toast: vi.fn(), refetch: vi.fn(), fetchNext: vi.fn(), query: {} as Record<string, unknown> }));
vi.mock("@tanstack/react-query", () => ({ useInfiniteQuery: () => mocks.query, useQueryClient: () => ({ invalidateQueries: mocks.invalidate }), queryOptions: <T,>(value: T) => value }));
vi.mock("@multica/core/api", () => ({ api: { enableGongfengRepository: mocks.enable, disableGongfengRepository: mocks.disable } }));
vi.mock("sonner", () => ({ toast: { error: mocks.toast } }));
import { GongfengRepositoryPicker } from "./gongfeng-repository-picker";

const connection: VCSConnection = { id: "conn", workspace_id: "ws", provider: "gongfeng", instance_url: "https://git.code.tencent.com", account_login: "alice", webhook_url: "https://multica.example/hook", webhook_path: "/hook", created_at: "" };
const repo: GongfengRepository = { id: 55, path: "acme/widget", web_url: "https://git.code.tencent.com/acme/widget", clone_url: "git@git.code.tencent.com:acme/widget.git", description: "", default_branch: "main", archived: false, syncing: false, sync_error: "", synced_at: null, webhook_configured: false };
beforeEach(() => {
  vi.clearAllMocks();
  mocks.query = { data: { pages: [{ repositories: [repo, { ...repo, id: 56, path: "acme/archived", archived: true }], selected: [] }] }, isPending: false, error: null, hasNextPage: true, refetch: mocks.refetch, fetchNextPage: mocks.fetchNext };
});
it("enables the selected repository and keeps archived repositories disabled", async () => {
  const user = userEvent.setup();
  renderWithI18n(<GongfengRepositoryPicker wsId="ws" connection={connection} onClose={vi.fn()} />);
  expect(screen.getByRole("checkbox", { name: /acme\/archived/ })).toHaveAttribute("aria-disabled", "true");
  await user.click(screen.getByRole("checkbox", { name: "acme/widget" }));
  await user.click(screen.getByRole("button", { name: "Enable sync" }));
  await waitFor(() => expect(mocks.enable).toHaveBeenCalledWith("ws", "conn", 55));
  expect(mocks.invalidate).toHaveBeenCalled();
  await user.click(screen.getByRole("button", { name: "Load more" }));
  expect(mocks.fetchNext).toHaveBeenCalledOnce();
});
it("imports already-enabled repositories without enabling them twice", async () => {
  const onImport = vi.fn().mockResolvedValue(true); const onClose = vi.fn();
  mocks.query.data = { pages: [{ repositories: [repo], selected: [{ ...repo, webhook_configured: true }] }] };
  const user = userEvent.setup();
  renderWithI18n(<GongfengRepositoryPicker wsId="ws" connection={connection} onClose={onClose} onImport={onImport} />);
  expect(screen.getByText("Webhook configured")).toBeVisible();
  await user.click(screen.getByRole("checkbox", { name: "acme/widget" }));
  await user.click(screen.getByRole("button", { name: "Add repositories" }));
  await waitFor(() => expect(onClose).toHaveBeenCalledOnce());
  expect(mocks.enable).not.toHaveBeenCalled(); expect(onImport).toHaveBeenCalledWith([repo]);
});
it("preserves selections when saving fails and offers retry for a failed sync", async () => {
  mocks.enable.mockRejectedValueOnce(new Error("Permission denied"));
  const onClose = vi.fn();const user = userEvent.setup();
  renderWithI18n(<GongfengRepositoryPicker wsId="ws" connection={connection} onClose={onClose} />);
  await user.click(screen.getByRole("checkbox", { name: "acme/widget" }));
  await user.click(screen.getByRole("button", { name: "Enable sync" }));
  await waitFor(() => expect(mocks.toast).toHaveBeenCalledWith("Permission denied"));
  expect(screen.getByRole("checkbox", { name: "acme/widget" })).toBeChecked(); expect(onClose).not.toHaveBeenCalled();
});
it("exposes failed repository sync and its retry action", async () => {
  mocks.query.data = { pages: [{ repositories: [repo], selected: [{ ...repo, sync_error: "Webhook permission denied" }] }] };
  const user = userEvent.setup();
  renderWithI18n(<GongfengRepositoryPicker wsId="ws" connection={connection} onClose={vi.fn()} />);
  expect(screen.getByText("Webhook permission denied")).toBeVisible();
  expect(screen.getByText("Webhook not configured")).toBeVisible();
  await user.click(screen.getByRole("button", { name: "Retry sync: acme/widget" }));
  await waitFor(() => expect(mocks.enable).toHaveBeenCalledWith("ws", "conn", 55, true));
});
