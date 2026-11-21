// @vitest-environment node
import { describe, expect, it } from "vitest";
import { composerActions, type ComposerActionInput } from "./composer-actions";

const chatDuringReply: ComposerActionInput = {
  isSending: true,
  allowSubmitWhileSending: true,
  hasStop: true,
  hasQueueAction: true,
  hasContent: true,
  hasInFlightUpload: false,
  submitting: false,
  disabled: false,
};

describe("composerActions", () => {
  it("offers Send and Queue for content typed during a reply", () => {
    expect(composerActions(chatDuringReply)).toEqual({
      canSend: true,
      showStop: false,
      showQueue: true,
    });
  });

  it("keeps Stop for an empty composer during a reply", () => {
    expect(composerActions({ ...chatDuringReply, hasContent: false })).toEqual({
      canSend: false,
      showStop: true,
      showQueue: false,
    });
  });

  it("keeps Stop reachable while an attachment uploads", () => {
    expect(composerActions({ ...chatDuringReply, hasInFlightUpload: true })).toEqual({
      canSend: false,
      showStop: true,
      showQueue: false,
    });
  });

  it("holds a disabled Send, not Stop, while the send is in flight", () => {
    expect(composerActions({
      ...chatDuringReply,
      hasContent: false,
      submitting: true,
    })).toEqual({ canSend: false, showStop: false, showQueue: false });
  });

  it("stays stop-only during a reply when the server cannot take sends", () => {
    expect(composerActions({
      ...chatDuringReply,
      allowSubmitWhileSending: false,
    })).toEqual({ canSend: false, showStop: true, showQueue: false });
  });

  it("offers Send without a Stop control while the head has not started", () => {
    // Chat withholds Stop for a queued head; content can still be sent.
    expect(composerActions({ ...chatDuringReply, hasStop: false })).toEqual({
      canSend: true,
      showStop: false,
      showQueue: true,
    });
  });

  it("sends normally when idle, without a queue action", () => {
    expect(composerActions({ ...chatDuringReply, isSending: false })).toEqual({
      canSend: true,
      showStop: false,
      showQueue: false,
    });
  });

  it("blocks every send while disabled", () => {
    expect(composerActions({ ...chatDuringReply, disabled: true }).canSend).toBe(false);
  });
});
