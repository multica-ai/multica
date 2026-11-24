import { fireEvent, renderHook, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "@multica/core/api";
import type { ReactNode } from "react";
import { I18nProvider } from "@multica/core/i18n/react";
import { getTestResources, renderWithI18n } from "../test/i18n";
import { StorageLimitNotice, storageLimitRefusal, useStorageLimitToast } from "./storage-limit-notice";

const h = vi.hoisted(() => ({
  workspace: { id: "ws-1", slug: "acme", name: "Acme Labs" } as { id: string; slug: string; name: string } | null,
  role: "owner" as "owner" | "admin" | "member",
  plan: "free" as string | undefined,
  push: vi.fn(),
  toastError: vi.fn(),
}));

vi.mock("@multica/core/paths", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/paths")>("@multica/core/paths");
  return {
    ...actual,
    useCurrentWorkspace: () => h.workspace,
    useWorkspaceSlug: () => h.workspace?.slug ?? null,
  };
});
vi.mock("@multica/core/auth", () => {
  const useAuthStore = (select: (state: { user: { id: string } }) => unknown) => select({ user: { id: "u1" } });
  useAuthStore.getState = () => ({ user: { id: "u1" } });
  return { useAuthStore };
});
vi.mock("@tanstack/react-query", async () => {
  const actual = await vi.importActual<typeof import("@tanstack/react-query")>("@tanstack/react-query");
  return {
    ...actual,
    useQuery: (options: { queryKey: readonly unknown[]; enabled?: boolean }) => {
      const key = options.queryKey;
      if (options.enabled === false) return { data: undefined };
      if (key[0] === "workspaces" && key[2] === "members") {
        return { data: [{ user_id: "u1", role: h.role }] };
      }
      if (key[0] === "workspace-subscriptions" && key[2] === "summary") {
        return { data: h.plan ? { entitlement: { plan: h.plan } } : undefined };
      }
      return { data: undefined };
    },
  };
});
vi.mock("sonner", () => ({ toast: { error: h.toastError } }));
vi.mock("../navigation", () => ({
  useOptionalNavigation: () => ({ pathname: "/acme/issues", push: h.push }),
}));

const refusal = { filename: "design-v3.fig", usedBytes: 1_000_000_000, limitBytes: 1_000_000_000 };

function storageError() {
  return new ApiError("full", 403, "Forbidden", {
    code: "storage_limit_reached",
    used_bytes: 1_000_000_000,
    limit_bytes: 1_000_000_000,
  });
}

describe("StorageLimitNotice", () => {
  beforeEach(() => {
    h.workspace = { id: "ws-1", slug: "acme", name: "Acme Labs" };
    h.role = "owner";
    h.plan = "free";
    h.push.mockReset();
    h.toastError.mockReset();
  });

  it.each([
    ["free", "Upgrade to Pro", "/acme/settings?tab=billing&upgrade=pro"],
    ["pro", "Upgrade to Business", "/acme/settings?tab=billing&upgrade=business"],
    ["business", "Manage billing", "/acme/settings?tab=billing"],
  ])("offers a %s admin the next step for storage", (plan, label, href) => {
    h.plan = plan;
    renderWithI18n(<StorageLimitNotice refusal={refusal} onDismiss={() => {}} />);

    expect(screen.getByText("design-v3.fig wasn't uploaded")).toBeTruthy();
    expect(
      screen.getByText("Attachment storage is full (1 GB / 1 GB). Existing attachments aren't affected."),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: label }));
    expect(h.push).toHaveBeenCalledWith(href);
  });

  it("tells a member to ask an admin, with no upgrade button", () => {
    h.role = "member";
    renderWithI18n(<StorageLimitNotice refusal={refusal} onDismiss={() => {}} />);

    expect(
      screen.getByText(
        "Attachment storage is full. Ask an admin to upgrade the plan, or delete attachments you no longer need.",
      ),
    ).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Upgrade|Manage billing|View plans/ })).toBeNull();
  });

  it("names a refused sub-issue copy instead of a file", () => {
    renderWithI18n(<StorageLimitNotice refusal={{ ...refusal, kind: "copy" }} onDismiss={() => {}} />);
    expect(screen.getByText("The sub-issue wasn't created: its source attachments couldn't be copied")).toBeTruthy();
  });

  it("dismisses", () => {
    const onDismiss = vi.fn();
    renderWithI18n(<StorageLimitNotice refusal={refusal} onDismiss={onDismiss} />);
    fireEvent.click(screen.getByRole("button", { name: "Dismiss" }));
    expect(onDismiss).toHaveBeenCalledTimes(1);
  });
});

describe("storageLimitRefusal", () => {
  it("is null for any other upload failure", () => {
    expect(storageLimitRefusal(new ApiError("too big", 413, "Too Large", {}), "a.png")).toBeNull();
    expect(storageLimitRefusal(storageError(), "a.png")).toEqual({
      filename: "a.png",
      usedBytes: 1_000_000_000,
      limitBytes: 1_000_000_000,
    });
  });
});

describe("useStorageLimitToast", () => {
  beforeEach(() => {
    h.toastError.mockReset();
    h.role = "owner";
    h.plan = "free";
    h.workspace = { id: "ws-1", slug: "acme", name: "Acme Labs" };
  });

  function seededClient() {
    const client = new QueryClient();
    client.setQueryData(["workspaces", "list"], [h.workspace]);
    client.setQueryData(["workspaces", "ws-1", "members"], [{ user_id: "u1", role: h.role }]);
    client.setQueryData(["workspace-subscriptions", "ws-1", "summary"], { entitlement: { plan: h.plan } });
    return client;
  }

  function wrap(client: QueryClient = new QueryClient()) {
    function Wrapper({ children }: { children: ReactNode }) {
      return (
        <QueryClientProvider client={client}>
          <I18nProvider locale="en" resources={getTestResources("en")}>
            {children}
          </I18nProvider>
        </QueryClientProvider>
      );
    }
    return Wrapper;
  }

  it("toasts a storage refusal with the admin's action and leaves other failures to the caller", async () => {
    const { result } = renderHook(() => useStorageLimitToast(), { wrapper: wrap(seededClient()) });
    expect(result.current(new Error("network"), "a.png")).toBe(false);

    expect(result.current(storageError(), "a.png")).toBe(true);
    await waitFor(() => expect(h.toastError).toHaveBeenCalledTimes(1));
    expect(h.toastError).toHaveBeenCalledWith(
      "a.png wasn't uploaded",
      expect.objectContaining({
        description: "Attachment storage is full (1 GB / 1 GB). Existing attachments aren't affected.",
        action: expect.objectContaining({ label: "Upgrade to Pro" }),
      }),
    );
  });

  it("gives a member the ask-an-admin copy without an action", async () => {
    h.role = "member";
    const { result } = renderHook(() => useStorageLimitToast(), { wrapper: wrap(seededClient()) });
    result.current(storageError(), "a.png");
    await waitFor(() => expect(h.toastError).toHaveBeenCalledTimes(1));
    expect(h.toastError).toHaveBeenCalledWith(
      "a.png wasn't uploaded",
      expect.objectContaining({
        description:
          "Attachment storage is full. Ask an admin to upgrade the plan, or delete attachments you no longer need.",
        action: undefined,
      }),
    );
  });

  it("reports a refusal outside a workspace with the member copy", async () => {
    h.workspace = null;
    const { result } = renderHook(() => useStorageLimitToast(), { wrapper: wrap() });
    expect(result.current(storageError(), "a.png")).toBe(true);
    await waitFor(() => expect(h.toastError).toHaveBeenCalledTimes(1));
    expect(h.toastError).toHaveBeenCalledWith("a.png wasn't uploaded", expect.objectContaining({ action: undefined }));
  });
});
