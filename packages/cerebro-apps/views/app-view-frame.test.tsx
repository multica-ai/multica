// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest";
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const callAppSdk = vi.fn();
vi.mock("../core/api", () => ({ callAppSdk: (...args: unknown[]) => callAppSdk(...args) }));
import { AppViewFrame } from "./app-view-frame";

describe("AppViewFrame", () => {
  afterEach(cleanup);
  beforeEach(() => {
    callAppSdk.mockReset();
    callAppSdk.mockResolvedValue({ formatted: "MILK" });
  });

  it("keeps app views in an opaque sandbox without parent-origin access", () => {
    render(<AppViewFrame appId="app-a" version="1.0.0" title="Approve formatting" src="https://apps.example/apps/a/1.0.0/views/approve" />);
    const frame = screen.getByTitle("Approve formatting");
    expect(frame).toHaveAttribute("sandbox", "allow-scripts allow-forms");
    expect(frame.getAttribute("sandbox")).not.toContain("allow-same-origin");
  });

  it("brokers only requests for the mounted app and returns the result to that frame", async () => {
    render(<AppViewFrame appId="app-a" version="1.0.0" title="App A" src="https://apps.example/apps/a/1.0.0/" />);
    const frame = screen.getByTitle("App A") as HTMLIFrameElement;
    const reply = vi.spyOn(frame.contentWindow!, "postMessage");

    act(() => window.dispatchEvent(new MessageEvent("message", {
      source: frame.contentWindow,
      origin: "null",
      data: {
        type: "multica.app-sdk.request",
        requestId: "request-1",
        appId: "app-a",
        version: "1.0.0",
        method: "workers.invoke",
        args: [{ ingredients: "milk" }],
      },
    })));

    await waitFor(() => expect(callAppSdk).toHaveBeenCalledWith({
      appId: "app-a",
      version: "1.0.0",
      method: "workers.invoke",
      args: [{ ingredients: "milk" }],
    }, "https://apps.example"));
    expect(reply).toHaveBeenCalledWith({
      type: "multica.app-sdk.response",
      requestId: "request-1",
      appId: "app-a",
      version: "1.0.0",
      ok: true,
      value: { formatted: "MILK" },
    }, "*");
  });

  it("ignores a request that claims another app identity", () => {
    render(<AppViewFrame appId="app-a" version="1.0.0" title="App A" src="https://apps.example/apps/a/1.0.0/" />);
    const frame = screen.getByTitle("App A") as HTMLIFrameElement;
    act(() => window.dispatchEvent(new MessageEvent("message", {
      source: frame.contentWindow,
      origin: "null",
      data: { type: "multica.app-sdk.request", requestId: "request-2", appId: "app-b", version: "1.0.0", method: "registry.token", args: [] },
    })));
    expect(callAppSdk).not.toHaveBeenCalled();
  });

  it("ignores methods outside the public app SDK", () => {
    render(<AppViewFrame appId="app-a" version="1.0.0" title="App A" src="https://apps.example/apps/a/1.0.0/" />);
    const frame = screen.getByTitle("App A") as HTMLIFrameElement;
    act(() => window.dispatchEvent(new MessageEvent("message", {
      source: frame.contentWindow,
      origin: "null",
      data: { type: "multica.app-sdk.request", requestId: "request-3", appId: "app-a", version: "1.0.0", method: "admin.delete", args: [] },
    })));
    expect(callAppSdk).not.toHaveBeenCalled();
  });
});
