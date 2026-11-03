import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, expect, it, vi } from "vitest";
import { LocalDeviceProvider, type LocalDevice } from "../platform/local-device";
import { WebResourcePreview } from "./local-resource-preview";
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

it("requires a source conversation before opening another device's local address", () => {
  vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} });
  const request = vi.fn(async (_scope: unknown, _command: { action: string }) => null);
  Object.defineProperty(window, "desktopAPI", { configurable: true, value: { resourcePreview: { request, subscribe: () => () => {}, listChromeProfiles: vi.fn(async () => []) }, openExternal: vi.fn() } });
  const device = { id: "peer-1", name: "Mac Studio", transport: {} } as unknown as LocalDevice;
  render(<QueryClientProvider client={new QueryClient()}><LocalDeviceProvider device={device}>
    <WebResourcePreview id="web:1" url="http://localhost:3000/" scope={{ userId: "u", workspaceId: "w" }} active chinese onChange={() => {}} onReference={() => {}} />
  </LocalDeviceProvider></QueryClientProvider>);
  expect(screen.getByRole("alert")).toHaveTextContent("请先选择来源会话");
  expect(request.mock.calls.some(([, command]) => command.action === "show")).toBe(false);
  vi.unstubAllGlobals();
});

it.each(["http://localhost:3000/", "http://192.168.1.10:3000/"])("carries the source device into the preview and blocks external service navigation: %s", async (url) => {
  vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} });
  const request = vi.fn(async () => null);
  Object.defineProperty(window, "desktopAPI", { configurable: true, value: { resourcePreview: { request, subscribe: () => () => {}, listChromeProfiles: vi.fn(async () => []) }, openExternal: vi.fn() } });
  const device = { id: "peer-1", name: "Mac Studio", transport: {} } as unknown as LocalDevice;
  render(<QueryClientProvider client={new QueryClient()}><LocalDeviceProvider device={device}><WebResourcePreview id="web:2" url={url} conversationId="conversation" scope={{ userId: "u", workspaceId: "w" }} active chinese onChange={() => {}} onReference={() => {}} /></LocalDeviceProvider></QueryClientProvider>);
  await waitFor(() => expect(request).toHaveBeenCalledWith({ userId: "u", workspaceId: "w" }, expect.objectContaining({ action: "show", url, target: { deviceId: "peer-1", conversationId: "conversation" } })));
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  for (const button of screen.getAllByRole("button", { name: "在浏览器中打开" })) expect(button).toBeDisabled();
  vi.unstubAllGlobals();
});

it.each([true, false])("shows a destination error without claiming a webpage requires loopback (Chinese: %s)", (chinese) => {
  vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} });
  Object.defineProperty(window, "desktopAPI", { configurable: true, value: { resourcePreview: {
    request: vi.fn(async () => null), listChromeProfiles: vi.fn(async () => []),
    subscribe: (callback: (state: unknown) => void) => {
      callback({ id: "web:error", url: "https://example.test/", title: "", loading: false, canGoBack: false, canGoForward: false, error: "preview_destination_denied: Invalid address" }); return () => {};
    },
  } } });
  const device = { id: "peer-1", transport: {} } as unknown as LocalDevice;
  render(<QueryClientProvider client={new QueryClient()}><LocalDeviceProvider device={device}><WebResourcePreview id="web:error" url="https://example.test/" conversationId="conversation" scope={{ userId: "u", workspaceId: "w" }} active chinese={chinese} onChange={() => {}} onReference={() => {}} /></LocalDeviceProvider></QueryClientProvider>);
  expect(screen.getByRole("alert")).toHaveTextContent(chinese ? "此地址不支持预览，请检查网页地址。" : "This address cannot be previewed. Check the webpage address.");
});
