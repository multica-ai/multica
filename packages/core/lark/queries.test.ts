// @vitest-environment node
import { expect, it, vi } from "vitest";
import { QueryClient, QueryObserver } from "@tanstack/react-query";
import { larkInstallationsOptions, larkKeys } from "./queries";
import { api } from "../api";
import type { ListLarkInstallationsResponse } from "../types";
vi.mock("../api", () => ({ api: { listLarkInstallations: vi.fn() } }));

it("refreshes mounted pending evidence and stops polling after verification", async () => {
  vi.useFakeTimers();
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const pending: ListLarkInstallationsResponse = { configured: true, installations: [{ id: "inst", workspace_id: "ws", agent_id: "a", app_id: "app", bot_open_id: "bot", installer_user_id: "user", status: "active", installed_at: "", created_at: "", updated_at: "" }] };
  vi.mocked(api.listLarkInstallations).mockResolvedValueOnce(pending).mockResolvedValue({ ...pending, installations: [{ ...pending.installations[0]!, first_message_received_at: "2026-10-29T00:00:00Z" }] });
  // QueryObserver timers do not run on the server. Inspect the actual query's
  // interval policy while verifying that a refetch updates the shared cache.
  const opts = larkInstallationsOptions("ws");
  const observer = new QueryObserver(client, opts);
  const unsubscribe = observer.subscribe(() => {});
  try {
    await client.fetchQuery(opts);
    const query = observer.getCurrentQuery();
    expect(typeof opts.refetchInterval).toBe("function");
    if (typeof opts.refetchInterval !== "function") throw new Error("missing pending refresh policy");
    expect(opts.refetchInterval(query)).toBe(5000);
    await observer.refetch();
    expect(client.getQueryData<ListLarkInstallationsResponse>(larkKeys.installations("ws"))?.installations[0]?.first_message_received_at).toBeTruthy();
    expect(opts.refetchInterval(query)).toBe(false);
    client.setQueryData(larkKeys.installations("ws"), { ...pending, installations: [{ ...pending.installations[0]!, status: "revoked" }] });
    expect(opts.refetchInterval(query)).toBe(false);
  } finally { unsubscribe(); client.clear(); vi.useRealTimers(); }
});
