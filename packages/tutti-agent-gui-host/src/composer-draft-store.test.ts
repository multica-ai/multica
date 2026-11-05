// @vitest-environment jsdom
import { afterEach,expect,it,vi } from "vitest";
import { createComposerDraftStore } from "./composer-draft-store";

afterEach(() => {
  sessionStorage.clear();
  vi.restoreAllMocks();
});

it("restores only valid text scopes from stored data", () => {
  sessionStorage.setItem(
    "multica:composer-drafts:v1:test",
    JSON.stringify({
      "session:a": "  first\nsecond  ",
      home: "home",
      "session:empty": "",
      "session:bad": {},
      unexpected: "ignored"
    })
  );
  expect(
    createComposerDraftStore(sessionStorage, "test").getState().drafts
  ).toEqual({
    "session:a": [{ type: "text", text: "  first\nsecond  " }],
    home: [{ type: "text", text: "home" }]
  });
});

it("keeps consecutive functional edits atomic and persists before notifying subscribers", () => {
  const store = createComposerDraftStore(sessionStorage, "test");
  const persisted: unknown[] = [];
  store.subscribe(() =>
    persisted.push(
      JSON.parse(sessionStorage.getItem("multica:composer-drafts:v1:test")!)
    )
  );
  store
    .getState()
    .setDrafts(() => ({ "session:a": [{ type: "text", text: "A" }] }));
  store.getState().setDrafts((current) => ({
    ...current,
    "session:b": [{ type: "text", text: "B" }]
  }));
  expect(persisted).toEqual([
    { "session:a": "A" },
    { "session:a": "A", "session:b": "B" }
  ]);
});

it("keeps upload payloads out of session storage", () => {
  const store = createComposerDraftStore(sessionStorage, "test");
  store.getState().setDrafts({
    "session:a": [
      { type: "text", text: "A" },
      {
        type: "image",
        id: "image",
        name: "image.png",
        mimeType: "image/png",
        data: "private-image-data",
        previewUrl: "blob:test-image"
      }
    ]
  });
  expect(
    JSON.parse(sessionStorage.getItem("multica:composer-drafts:v1:test")!)
  ).toEqual({ "session:a": "A" });
});

it("keeps editing usable when saved data is malformed or storage is full", () => {
  const warning = vi.spyOn(console, "warn").mockImplementation(() => {});
  sessionStorage.setItem("multica:composer-drafts:v1:test", "{");
  const store = createComposerDraftStore(sessionStorage, "test");
  expect(store.getState().drafts).toEqual({});
  vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
    throw new DOMException("Full", "QuotaExceededError");
  });
  store
    .getState()
    .setDrafts({ home: [{ type: "text", text: "Still editable" }] });
  expect(store.getState().drafts.home?.[0].text).toBe("Still editable");
  expect(warning).toHaveBeenCalledTimes(2);
});

it("moves a legacy session draft to its canonical id and retains conflicting text", () => {
  sessionStorage.setItem(
    "multica:composer-drafts:v1:test",
    JSON.stringify({ "session:old": "Old draft" })
  );
  expect(
    createComposerDraftStore(sessionStorage, "test", {
      old: "server"
    }).getState().drafts
  ).toEqual({ "session:server": [{ type: "text", text: "Old draft" }] });
  sessionStorage.setItem(
    "multica:composer-drafts:v1:test",
    JSON.stringify({
      "session:old": "Old draft",
      "session:server": "New draft"
    })
  );
  const drafts = createComposerDraftStore(sessionStorage, "test", {
    old: "server"
  }).getState().drafts;
  expect(drafts["session:server"]?.[0].text).toBe("New draft");
  expect(drafts["session:old"]?.[0].text).toBe("Old draft");
});
