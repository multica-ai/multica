import { Toaster } from "sonner";
import { useState } from "react";
import { act, cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiClient, setApiInstance } from "@multica/core/api";
import { workspaceListOptions } from "@multica/core/workspace/queries";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import { afterEach, describe, expect, it, vi } from "vitest";
import { NavigationProvider } from "../../../navigation";
import { renderWithI18n } from "../../../test/i18n";
import { LabelPicker } from "./label-picker";

const wsId = "22222222-2222-4222-8222-222222222222";
const alpha = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa";
const beta = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb";
const gamma = "cccccccc-cccc-4ccc-8ccc-cccccccccccc";
const makeLabel = (id: string, name: string) => ({
  id, name, workspace_id: wsId, color: "#3b82f6",
  created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z",
});
let qc: QueryClient;

afterEach(() => {
  cleanup();
  qc?.clear();
  vi.unstubAllGlobals();
});

// Keep the production picker, Query mutations, providers and ApiClient. Only
// fetch is synthetic, so resolving POST exercises the real mutation callbacks.
async function setup() {
  let labels = [makeLabel(alpha, "Alpha"), makeLabel(beta, "Beta")];
  let finishCreate: (success: boolean) => void = () => { throw new Error("No pending create"); };
  const writes: unknown[] = [];
  vi.stubGlobal("fetch", vi.fn(async (input: string, init?: RequestInit) => {
    const url = new URL(input);
    const method = init?.method ?? "GET";
    const json = (value: unknown, status = 200) => new Response(JSON.stringify(value), { status });
    if (url.origin !== "http://127.0.0.1") throw new Error(`Unexpected origin: ${url.origin}`);
    if (method === "GET" && url.pathname === "/api/workspaces") {
      return json([{ id: wsId, slug: "fixture", name: "Fixture", role: "owner", created_at: "2026-01-01T00:00:00Z" }]);
    }
    if (method === "GET" && url.pathname === "/api/labels") {
      return json({ labels, total: labels.length });
    }
    if (method === "POST" && url.pathname === "/api/labels") {
      writes.push(JSON.parse(String(init?.body)));
      return new Promise<Response>((resolve) => {
        finishCreate = (success) => {
          const created = makeLabel(gamma, "Gamma");
          if (success) labels = [...labels, created];
          resolve(success ? json(created, 201) : json({ error: "Create failed" }, 500));
        };
      });
    }
    throw new Error(`Unexpected request: ${method} ${url.pathname}`);
  }));
  setApiInstance(new ApiClient("http://127.0.0.1"));
  qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  await qc.ensureQueryData(workspaceListOptions());
  function Draft() {
    const [ids, setIds] = useState<string[]>([]);
    return <>
      <LabelPicker selectedIds={ids} onSelectedIdsChange={setIds} defaultOpen triggerRender={<button type="button" />} />
      <output data-testid="selection">{ids.join(",")}</output>
    </>;
  }
  renderWithI18n(
    <QueryClientProvider client={qc}>
      <WorkspaceSlugProvider slug="fixture">
        <NavigationProvider value={{ push: vi.fn(), replace: vi.fn(), back: vi.fn(), pathname: "/fixture/issues", searchParams: new URLSearchParams(), hash: "", getShareableUrl: (p) => p }}>
          <Draft />
          <Toaster />
        </NavigationProvider>
      </WorkspaceSlugProvider>
    </QueryClientProvider>,
  );
  const user = userEvent.setup();
  await screen.findByRole("button", { name: "Alpha" });
  const toggle = async (name: string) => user.click(within(screen.getByRole("dialog")).getByRole("button", { name }));
  const search = screen.getByPlaceholderText("Find or create a label…");
  const beginCreate = async () => {
    await user.type(search, "Gamma");
    await user.click(screen.getByRole("button", { name: /Create.*Gamma/ }));
    await waitFor(() => expect(writes).toHaveLength(1));
    await user.clear(search);
  };
  const finish = async (success = true) => { await act(async () => finishCreate(success)); };
  const expectSelection = async (ids: string[]) => {
    await waitFor(() => expect(screen.getByTestId("selection").textContent).toBe(ids.join(",")));
    const names = ids.map((id) => ({ [alpha]: "Alpha", [beta]: "Beta", [gamma]: "Gamma" })[id]);
    expect(Array.from(document.querySelectorAll('[data-slot="label-chip"]'), (chip) => chip.getAttribute("aria-label"))).toEqual(names);
  };
  return { toggle, beginCreate, finish, expectSelection, writes };
}

describe("LabelPicker pending draft creation", () => {
  it.each([
    { name: "unchanged selection", edits: [] as string[], expected: [alpha, gamma] },
    { name: "addition", edits: ["Beta"], expected: [alpha, beta, gamma] },
    { name: "removal", edits: ["Alpha"], expected: [gamma] },
    { name: "mixed edits", edits: ["Alpha", "Beta"], expected: [beta, gamma] },
    { name: "changed selection order", edits: ["Beta", "Alpha", "Alpha"], expected: [beta, alpha, gamma] },
  ])("appends once after $name", async ({ edits, expected }) => {
    const fixture = await setup();
    await fixture.toggle("Alpha");
    await fixture.beginCreate();
    for (const name of edits) await fixture.toggle(name);
    await fixture.finish();
    await fixture.expectSelection(expected);
    expect(fixture.writes).toEqual([expect.objectContaining({ name: "Gamma", resource_type: "issue" })]);
  });

  it("keeps intervening edits when creation fails", async () => {
    const fixture = await setup();
    await fixture.toggle("Alpha");
    await fixture.beginCreate();
    await fixture.toggle("Alpha");
    await fixture.toggle("Beta");
    await fixture.finish(false);
    await fixture.expectSelection([beta]);
    expect(await screen.findByText("Create failed")).toBeVisible();
    expect(fixture.writes).toEqual([expect.objectContaining({ name: "Gamma", resource_type: "issue" })]);
  });
});
