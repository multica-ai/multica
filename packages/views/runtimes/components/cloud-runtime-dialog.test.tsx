import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { toast } from "sonner";
import { I18nProvider } from "@multica/core/i18n/react";
import { cloudRuntimeKeys, type CloudRuntimeNode } from "@multica/core/runtimes";
import enRuntimes from "../../locales/en/runtimes.json";
import { CloudRuntimeDialog } from "./cloud-runtime-dialog";

const api = vi.hoisted(() => ({ list: vi.fn(), remove: vi.fn() }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/api", async (importOriginal) => ({
  ...await importOriginal<typeof import("@multica/core/api")>(),
  api: {
    listCloudRuntimeNodes: (...args: unknown[]) => api.list(...args),
    deleteCloudRuntimeNode: (...args: unknown[]) => api.remove(...args),
  },
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

const node: CloudRuntimeNode = {
  id: "node-1", owner_id: "owner-1", instance_id: "synthetic-instance",
  region: "test-region", instance_type: "t4g.medium", image_id: "", subnet_id: "",
  name: "Test cloud node", status: "running", tags: {}, metadata: {},
  created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z",
};

function deferred() {
  let resolve!: () => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<void>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

function renderDialog() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  const view = render(
    <I18nProvider locale="en" resources={{ en: { runtimes: enRuntimes } }}>
      <QueryClientProvider client={client}>
        <CloudRuntimeDialog onClose={vi.fn()} />
      </QueryClientProvider>
    </I18nProvider>,
  );
  return { ...view, client };
}

describe("CloudRuntimeDialog deletion feedback", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    api.list.mockResolvedValue([node]);
    vi.spyOn(window, "confirm").mockReturnValue(true);
  });

  for (const outcome of ["success", "failure"] as const) {
    it(`reports ${outcome} after the dialog unmounts`, async () => {
      const request = deferred();
      api.remove.mockReturnValue(request.promise);
      const view = renderDialog();
      const button = await screen.findByRole("button", { name: "Delete node" });
      fireEvent.click(button);
      await waitFor(() => expect(api.remove).toHaveBeenCalledWith(node.instance_id));
      await waitFor(() => expect(button).toBeDisabled());
      view.unmount();
      await act(async () => {
        if (outcome === "success") request.resolve();
        else request.reject(new Error("Synthetic delete failure"));
      });
      await waitFor(() => {
        if (outcome === "success") expect(toast.success).toHaveBeenCalledExactlyOnceWith("Cloud node deleted");
        else expect(toast.error).toHaveBeenCalledExactlyOnceWith("Synthetic delete failure");
      });
      view.client.clear();
    });
  }

  it("reports completion when a refreshed list removes the row before the request settles", async () => {
    const request = deferred();
    api.remove.mockReturnValue(request.promise);
    const view = renderDialog();
    fireEvent.click(await screen.findByRole("button", { name: "Delete node" }));
    await waitFor(() => expect(api.remove).toHaveBeenCalledOnce());
    await act(async () => {
      view.client.setQueriesData({ queryKey: cloudRuntimeKeys.nodes("ws-1") }, []);
    });
    await waitFor(() => expect(screen.queryByRole("button", { name: "Delete node" })).not.toBeInTheDocument());
    api.list.mockResolvedValue([]);
    await act(async () => request.resolve());
    await waitFor(() => expect(toast.success).toHaveBeenCalledExactlyOnceWith("Cloud node deleted"));
    view.unmount();
    view.client.clear();
  });

  it("does not delete when confirmation is cancelled", async () => {
    vi.mocked(window.confirm).mockReturnValue(false);
    const view = renderDialog();
    fireEvent.click(await screen.findByRole("button", { name: "Delete node" }));
    expect(window.confirm).toHaveBeenCalledWith(enRuntimes.cloud_runtime.delete_confirm);
    expect(api.remove).not.toHaveBeenCalled();
    view.unmount();
    view.client.clear();
  });
});
