// @vitest-environment jsdom
import { afterEach,expect,it,vi } from "vitest";
import { createDesktopPendingStore } from "./desktop-pending-store";

afterEach(() => {
  sessionStorage.clear();
  vi.restoreAllMocks();
});

const queues = [
  {
    agentSessionId: "a",
    prompts: [
      {
        id: "original",
        clientSubmitId: "original",
        createdAtUnixMs: 1,
        content: [{ type: "text" as const, text: "  中文 👋\nsecond  " }]
      }
    ]
  }
];

it("isolates pending submissions by the complete draft namespace and saves before notification", () => {
  const store = createDesktopPendingStore(sessionStorage, "scope-a");
  const read = () =>
    createDesktopPendingStore(sessionStorage, "scope-a").getState().queues;
  store.subscribe(() => expect(read()).toEqual(queues));
  store.getState().setQueues(queues);
  expect(
    createDesktopPendingStore(sessionStorage, "scope-b").getState().queues
  ).toEqual([]);
  expect(read()).toEqual(queues);
  expect(store.getState().durable).toBe(true);
});

it("ignores invalid stored entries and keeps the current submission usable if storage is full", () => {
  const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
  sessionStorage.setItem(
    "multica:desktop-pending:v1:scope",
    JSON.stringify([{ agentSessionId: "a", prompts: [{ id: "invalid" }] }])
  );
  expect(
    createDesktopPendingStore(sessionStorage, "scope").getState().queues
  ).toEqual([]);
  sessionStorage.setItem("multica:desktop-pending:v1:scope", "{");
  const store = createDesktopPendingStore(sessionStorage, "scope");
  vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
    throw new DOMException("Full", "QuotaExceededError");
  });
  store.getState().setQueues(queues);
  expect(store.getState().queues).toEqual(queues);
  expect(store.getState().durable).toBe(false);
  expect(warn).toHaveBeenCalledTimes(2);
});
