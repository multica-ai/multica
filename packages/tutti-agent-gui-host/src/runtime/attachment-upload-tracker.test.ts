// @vitest-environment node
import { describe,expect,it,vi } from "vitest";
import { updateAgentComposerDraft } from "../../../../vendor/tutti/packages/agent/gui/agent-gui/agentGuiNode/model/agentComposerDraft";
import { fakeHostServices } from "../../e2e-harness/fake-host-services";
import {
composerDraftAttachmentIds,
createAttachmentUploadTracker
} from "./attachment-upload-tracker";

function setup() {
  const request = vi.fn(
    async (_method: string, _params?: unknown) => undefined
  );
  const tracker = createAttachmentUploadTracker(
    fakeHostServices(request),
    "en"
  );
  return { request, tracker };
}

const settled = async (promise: Promise<unknown>) => {
  let state = "pending";
  void promise.then(
    () => {
      state = "resolved";
    },
    () => {
      state = "rejected";
    }
  );
  await new Promise((resolve) => setImmediate(resolve));
  return state;
};

describe("attachment upload tracker", () => {
  it("lets a send wait for the transfers it needs", async () => {
    const { tracker } = setup();
    tracker.track("a", "a.png");
    const waiting = tracker.ensure(["a", "already-stored"]);
    expect(await settled(waiting)).toBe("pending");
    tracker.settle({
      type: "attachment.upload",
      attachmentId: "a",
      state: "uploaded"
    });
    expect(await settled(waiting)).toBe("resolved");
  });

  it("fails a waiting send, which retries the transfer on its next attempt", async () => {
    const { request, tracker } = setup();
    tracker.track("a", "a.png");
    const waiting = tracker.ensure(["a"]);
    tracker.settle({
      type: "attachment.upload",
      attachmentId: "a",
      state: "failed",
      message: "offline"
    });
    await expect(waiting).rejects.toThrow("Could not upload a.png: offline");
    expect(request).not.toHaveBeenCalledWith(
      "host.showMessage",
      expect.anything()
    );
    const retrying = tracker.ensure(["a"]);
    await new Promise((resolve) => setImmediate(resolve));
    expect(request).toHaveBeenCalledWith("attachment.retry", {
      attachmentId: "a"
    });
    tracker.settle({
      type: "attachment.upload",
      attachmentId: "a",
      state: "uploaded"
    });
    await expect(retrying).resolves.toBeUndefined();
  });

  it("tells the user about a failure nothing is waiting for", () => {
    const { request, tracker } = setup();
    tracker.track("a", "a.png");
    tracker.settle({
      type: "attachment.upload",
      attachmentId: "a",
      state: "failed",
      message: "offline"
    });
    expect(request).toHaveBeenCalledWith("host.showMessage", {
      message: "Could not upload a.png: offline",
      tone: "error"
    });
  });

  it("accepts an outcome that overtook the announcement", async () => {
    const { tracker } = setup();
    tracker.settle({
      type: "attachment.upload",
      attachmentId: "a",
      state: "uploaded"
    });
    tracker.track("a", "a.png");
    expect(tracker.activeCount()).toBe(0);
    await expect(tracker.ensure(["a"])).resolves.toBeUndefined();
  });

  it("counts active transfers and notifies when the count changes", () => {
    const { tracker } = setup();
    const listener = vi.fn();
    tracker.subscribe(listener);
    tracker.track("a", "a.png");
    tracker.track("b", "b.png");
    expect(tracker.activeCount()).toBe(2);
    tracker.settle({
      type: "attachment.upload",
      attachmentId: "a",
      state: "uploaded"
    });
    expect(tracker.activeCount()).toBe(1);
    expect(listener).toHaveBeenCalledTimes(3);
  });

  it("cancels only what it tracks, and rejects anyone waiting", async () => {
    const { request, tracker } = setup();
    tracker.track("a", "a.png");
    const waiting = tracker.ensure(["a"]);
    tracker.remove(["a", "referenced-from-another-message"]);
    await expect(waiting).rejects.toThrow("removed");
    expect(request.mock.calls).toEqual([
      ["attachment.cancel", { attachmentId: "a" }]
    ]);
  });

  it("releases attachments a message took over without discarding them", () => {
    const { request, tracker } = setup();
    tracker.track("a", "a.png");
    tracker.settle({
      type: "attachment.upload",
      attachmentId: "a",
      state: "uploaded"
    });
    tracker.forget(["a"]);
    tracker.remove(["a"]);
    expect(request.mock.calls).toEqual([
      ["attachment.forget", { attachmentId: "a" }]
    ]);
  });

  it("finds attachments of every draft chip kind", () => {
    const draft = updateAgentComposerDraft(
      [{ type: "text", text: "" }] as never,
      {
        images: [
          { id: "1", name: "i", mimeType: "image/png", attachmentId: "image" }
        ],
        files: [
          { id: "2", name: "f", assetId: "file", url: "u", uploading: false }
        ],
        largeTexts: [
          {
            id: "3",
            name: "t",
            text: "x",
            sizeBytes: 1,
            assetId: "text",
            uploading: false
          }
        ]
      } as never
    );
    expect([...composerDraftAttachmentIds(draft)].sort()).toEqual([
      "file",
      "image",
      "text"
    ]);
  });
});
