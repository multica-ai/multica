// @vitest-environment node
import { describe, expect, it, vi } from "vitest";
import { confirmThenApply } from "./settings-confirm";

const scope = { workspaceId: "workspace", userId: "user" };
const request = (operation: string, input: Record<string, unknown> = {}) => ({ operation, scope, input });
const apply = { method: "POST", path: "/api/x", body: {} } as never;

describe("confirmThenApply", () => {
  it("writes only after the host accepts the request", async () => {
    const write = vi.fn(async () => ({ id: "w1" }));
    const declined = confirmThenApply({ confirm: async () => false, request: write });
    expect(await declined(request("settings_create_workflow"), apply, new AbortController().signal)).toEqual({ approved: false });
    expect(write).not.toHaveBeenCalled();
    const approved = confirmThenApply({ confirm: async () => true, request: write });
    expect(await approved(request("settings_create_workflow"), apply, new AbortController().signal)).toEqual({ approved: true, raw: { id: "w1" } });
    expect(await approved(request("settings_open_page"), null, new AbortController().signal)).toEqual({ approved: true, raw: null });
  });

  it("shows a new token only to the human and keeps it out of the result", async () => {
    const showSecret = vi.fn(async () => {});
    const create = confirmThenApply({ confirm: async () => true, request: async () => ({ id: "t1", token: "mul_secret" }), showSecret });
    const outcome = await create(request("settings_create_token", { name: "CI" }), apply, new AbortController().signal);
    expect(outcome).toEqual({ approved: true, raw: { id: "t1" } });
    expect(JSON.stringify(outcome)).not.toContain("mul_secret");
    expect(showSecret).toHaveBeenCalledWith("CI", "mul_secret", scope, expect.any(AbortSignal));
  });

  it("refuses to report a token it cannot display or the server did not return", async () => {
    const hidden = confirmThenApply({ confirm: async () => true, request: async () => ({ id: "t1", token: "mul_secret" }) });
    await expect(hidden(request("settings_create_token", { name: "CI" }), apply, new AbortController().signal)).rejects.toMatchObject({ code: "secret_display_unavailable" });
    const missing = confirmThenApply({ confirm: async () => true, request: async () => ({ id: "t1" }), showSecret: vi.fn() });
    await expect(missing(request("settings_create_token", { name: "CI" }), apply, new AbortController().signal)).rejects.toMatchObject({ code: "result_unconfirmed" });
  });
});
