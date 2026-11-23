import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "@multica/core/api";
import { I18nProvider } from "@multica/core/i18n/react";
import enChat from "../../locales/en/chat.json";
import { ChatDirectoryDialog } from "./chat-directory-dialog";

vi.mock("@multica/core/api", () => ({ api: { createRuntimeDirectoryRequest: vi.fn(), getRuntimeDirectoryRequest: vi.fn() } }));
const listing = (path: string, selectable = true) => ({ id: "request", runtime_id: "runtime", status: "completed" as const, path, error: null, result: { path, parent_path: "/home", entries: [{ name: "项目 files", path: "/home/me/项目 files" }], selectable } });
function mount() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  const onSelect = vi.fn();
  const rendered = render(<QueryClientProvider client={qc}><I18nProvider locale="en" resources={{ en: { chat: enChat } }}><ChatDirectoryDialog wsId="ws" runtimeId="runtime" daemonId="daemon" open onOpenChange={vi.fn()} onSelect={onSelect} /></I18nProvider></QueryClientProvider>);
  return { onSelect, ...rendered, qc };
}
beforeEach(() => { vi.mocked(api.createRuntimeDirectoryRequest).mockReset().mockResolvedValue(listing("/home/me")); });

describe("daemon directory picker", () => {
  it("browses children and sends a confirmed path without changing Unicode or spaces", async () => {
    const { onSelect } = mount();
    await screen.findByRole("button", { name: "项目 files" });
    vi.mocked(api.createRuntimeDirectoryRequest).mockResolvedValueOnce(listing("/home/me/项目 files"));
    fireEvent.click(screen.getByRole("button", { name: "项目 files" }));
    await waitFor(() => expect(screen.getByLabelText("Directory path")).toHaveValue("/home/me/项目 files"));
    await waitFor(() => expect(screen.getByRole("button", { name: "Use directory" })).toBeEnabled());
    fireEvent.click(screen.getByRole("button", { name: "Use directory" }));
    expect(onSelect).toHaveBeenCalledWith({ daemon_id: "daemon", path: "/home/me/项目 files" });
  });
  it("discards a late listing after switching the runtime", async () => {
    let resolve!: (value: ReturnType<typeof listing>) => void;
    vi.mocked(api.createRuntimeDirectoryRequest).mockImplementationOnce(() => new Promise((r) => { resolve = r; }));
    const { onSelect, rerender, qc } = mount();
    await waitFor(() => expect(api.createRuntimeDirectoryRequest).toHaveBeenCalled());
    vi.mocked(api.createRuntimeDirectoryRequest).mockResolvedValueOnce({ ...listing("/new-machine"), runtime_id: "other-runtime" });
    rerender(<QueryClientProvider client={qc}><I18nProvider locale="en" resources={{ en: { chat: enChat } }}><ChatDirectoryDialog wsId="ws" runtimeId="other-runtime" daemonId="other-daemon" open onOpenChange={vi.fn()} onSelect={onSelect} /></I18nProvider></QueryClientProvider>);
    await waitFor(() => expect(screen.getByLabelText("Directory path")).toHaveValue("/new-machine"));
    await act(async () => resolve(listing("/old-machine")));
    fireEvent.click(screen.getByRole("button", { name: "Use directory" }));
    expect(onSelect).toHaveBeenCalledWith({ daemon_id: "other-daemon", path: "/new-machine" });
  });
  it("shows polling errors and keeps selection disabled", async () => {
    vi.mocked(api.createRuntimeDirectoryRequest).mockResolvedValue({ ...listing("/home/me"), status: "pending", result: null });
    vi.mocked(api.getRuntimeDirectoryRequest).mockRejectedValue(new Error("offline"));
    mount();
    await screen.findByRole("alert");
    expect(screen.getByRole("button", { name: "Use directory" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Browse" })).toBeEnabled();
  });
  it("validates a directly entered path before selecting it", async () => {
    const { onSelect } = mount();
    await waitFor(() => expect(screen.getByRole("button", { name: "Use directory" })).toBeEnabled());
    fireEvent.change(screen.getByLabelText("Directory path"), { target: { value: "/typed/项目 files" } });
    vi.mocked(api.createRuntimeDirectoryRequest).mockResolvedValueOnce(listing("/typed/项目 files"));
    fireEvent.click(screen.getByRole("button", { name: "Browse" }));
    await waitFor(() => expect(screen.getByRole("button", { name: "Use directory" })).toBeEnabled());
    fireEvent.click(screen.getByRole("button", { name: "Use directory" }));
    expect(onSelect).toHaveBeenCalledWith({ daemon_id: "daemon", path: "/typed/项目 files" });
  });
  it("disables selection when path text changes until the new path is confirmed", async () => {
    mount();
    await waitFor(() => expect(screen.getByRole("button", { name: "Use directory" })).toBeEnabled());
    fireEvent.change(screen.getByLabelText("Directory path"), { target: { value: "/unconfirmed" } });
    expect(screen.getByRole("button", { name: "Use directory" })).toBeDisabled();
  });
  it("disables selection while a new listing is pending and after validation fails", async () => {
    mount();
    await waitFor(() => expect(screen.getByRole("button", { name: "Use directory" })).toBeEnabled());
    let resolve!: (value: ReturnType<typeof listing>) => void;
    vi.mocked(api.createRuntimeDirectoryRequest).mockImplementationOnce(() => new Promise((r) => { resolve = r; }));
    fireEvent.click(screen.getByRole("button", { name: "项目 files" }));
    expect(screen.getByRole("button", { name: "Use directory" })).toBeDisabled();
    await waitFor(() => expect(resolve).toBeTypeOf("function"));
    await act(async () => resolve(listing("/home/me/项目 files", false)));
    expect(screen.getByRole("button", { name: "Use directory" })).toBeDisabled();
  });
});
