// @vitest-environment node
import { describe,expect,it,vi } from "vitest";
import { fakeHostServices } from "../e2e-harness/fake-host-services";
import { createHostServicesConnection } from "./host-services-connection";

describe("direct host service lifetime", () => {
  it("refuses a queued call if its workspace changed before dispatch", async () => {
    let current = true;
    const run = vi.fn(async () => ({}));
    const connection = createHostServicesConnection(fakeHostServices(run), {
      current: () => current,
      interactive: () => true
    });
    const request = connection.client.workspace.load({});
    current = false;
    await expect(request).rejects.toMatchObject({ name: "AbortError" });
    expect(run).not.toHaveBeenCalled();
    connection.dispose();
  });

  it("rejects a result that returns after its workspace changed", async () => {
    let current = true;
    let resolve!: (value: unknown) => void;
    const run = vi.fn(
      () =>
        new Promise((done) => {
          resolve = done;
        })
    );
    const connection = createHostServicesConnection(fakeHostServices(run), {
      current: () => current,
      interactive: () => true
    });
    const request = connection.client.workspace.load({});
    await Promise.resolve();
    current = false;
    resolve({ agents: [], sessions: [] });
    await expect(request).rejects.toMatchObject({ name: "AbortError" });
    connection.dispose();
  });

  it("does not execute an aborted operation still waiting for dispatch", async () => {
    const run = vi.fn(async () => ({}));
    const connection = createHostServicesConnection(fakeHostServices(run), {
      current: () => true,
      interactive: () => true
    });
    const abort = new AbortController();
    const request = connection.client.workspace.load(
      {},
      { signal: abort.signal }
    );
    abort.abort();
    await expect(request).rejects.toMatchObject({ name: "AbortError" });
    expect(run).not.toHaveBeenCalled();
    connection.dispose();
  });
  it("releases request timers when the instance is disposed", async () => {
    vi.useFakeTimers();
    try {
      const connection = createHostServicesConnection(
        fakeHostServices(async () => new Promise(() => {})),
        { current: () => true, interactive: () => true }
      );
      const request = connection.client.workspace.load({});
      await Promise.resolve();
      expect(vi.getTimerCount()).toBe(1);
      connection.dispose();
      await expect(request).rejects.toMatchObject({ name: "AbortError" });
      expect(vi.getTimerCount()).toBe(0);
    } finally {
      vi.useRealTimers();
    }
  });
});
