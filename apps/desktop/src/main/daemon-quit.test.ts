// @vitest-environment node
import { describe, expect, it, vi } from "vitest";
import { createDaemonQuitHandler } from "./daemon-quit";

function setup() {
  const deps = {
    loadPrefs: vi.fn(async () => ({ autoStop: true })), pause: vi.fn(), resume: vi.fn(),
    stop: vi.fn(async () => true), confirmIgnore: vi.fn(async () => false), quit: vi.fn(),
  };
  const handler = createDaemonQuitHandler(deps);
  const request = () => { const event = { preventDefault: vi.fn() }; handler(event); return event; };
  return { deps, request };
}

describe("quit with daemon auto-stop", () => {
  it("synchronously blocks repeated quits until stopping finishes", async () => {
    const { deps, request } = setup();
    let finish!: (result: boolean) => void;
    deps.stop.mockReturnValue(new Promise((resolve) => { finish = resolve; }));
    expect(request().preventDefault).toHaveBeenCalledOnce();
    request();
    await vi.waitFor(() => expect(deps.stop).toHaveBeenCalledOnce());
    expect(deps.quit).not.toHaveBeenCalled();
    finish(true);
    await vi.waitFor(() => expect(deps.quit).toHaveBeenCalledOnce());
    expect(request().preventDefault).not.toHaveBeenCalled();
  });

  it("preserves the daemon when auto-stop is disabled", async () => {
    const { deps, request } = setup();
    deps.loadPrefs.mockResolvedValue({ autoStop: false });
    request();
    await vi.waitFor(() => expect(deps.quit).toHaveBeenCalledOnce());
    expect(deps.pause).not.toHaveBeenCalled();
    expect(deps.stop).not.toHaveBeenCalled();
  });

  it("resumes normal use when the task warning is cancelled", async () => {
    const { deps, request } = setup();
    deps.stop.mockResolvedValue(false);
    request();
    await vi.waitFor(() => expect(deps.resume).toHaveBeenCalledWith(true));
    expect(deps.quit).not.toHaveBeenCalled();
    expect(deps.confirmIgnore).not.toHaveBeenCalled();
  });

  it.each([false, true])("only quits after a stop failure when ignore is %s", async (ignore) => {
    const { deps, request } = setup();
    const error = new Error("OS refused termination");
    deps.stop.mockRejectedValue(error);
    deps.confirmIgnore.mockResolvedValue(ignore);
    request();
    await vi.waitFor(() => expect(deps.confirmIgnore).toHaveBeenCalledWith(error));
    await vi.waitFor(() => expect(ignore ? deps.quit : deps.resume).toHaveBeenCalledOnce());
    if (ignore) expect(deps.resume).not.toHaveBeenCalled();
    else {
      expect(deps.resume).toHaveBeenCalledWith(false);
      expect(deps.quit).not.toHaveBeenCalled();
    }
  });
});
