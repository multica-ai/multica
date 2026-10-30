// @vitest-environment node
import { describe, expect, it } from "vitest";
import { isBridgeEvent, isBridgeRequest, isBridgeResponse } from "@multica/plugin-sdk/protocol";

// Beside plugin-sdk-handshake.test.ts, and here for the same reason.
//
// The guards are the only thing standing between a hostile message and the
// bridge's handlers on both sides. Anything that is not exactly the agreed
// shape has to fall through, not be coerced into something close enough.

describe("bridge message guards", () => {
  it("accepts the shapes both sides agreed on", () => {
    expect(isBridgeRequest({ id: "r1", kind: "action", method: "GET", path: "/context" })).toBe(true);
    expect(isBridgeRequest({ id: "r1", kind: "ui.resize", height: 200 })).toBe(true);
    expect(isBridgeResponse({ id: "r1", ok: true, status: 200, data: {} })).toBe(true);
    expect(isBridgeResponse({ id: "r1", ok: false, status: 403, error: "nope" })).toBe(true);
    expect(isBridgeEvent({ kind: "theme", theme: {} })).toBe(true);
  });

  it("rejects anything that only looks close enough", () => {
    for (const value of [
      null,
      undefined,
      "action",
      42,
      {},
      { kind: "action", method: "GET", path: "/context" }, // no id to pair a reply to
      { id: 7, kind: "action", method: "GET", path: "/context" }, // id must be a string
      { id: "r1", kind: "action", method: "GET" }, // no path
      { id: "r1", kind: "ui.resize" }, // no height
      { id: "r1", kind: "ui.resize", height: "200" }, // height must be a number
      { id: "r1", kind: "navigate", url: "https://evil.test" }, // unknown kind
    ]) {
      expect(isBridgeRequest(value)).toBe(false);
    }
  });

  it("does not confuse a response with an event or vice versa", () => {
    // Both travel on the same port, so a mix-up would either resolve a call
    // with a theme payload or apply a response as design tokens.
    expect(isBridgeEvent({ id: "r1", ok: true, status: 200, data: {} })).toBe(false);
    expect(isBridgeResponse({ kind: "theme", theme: {} })).toBe(false);
  });

  it("rejects a response with no boolean outcome", () => {
    expect(isBridgeResponse({ id: "r1", status: 200 })).toBe(false);
    expect(isBridgeResponse({ id: "r1", ok: "true" })).toBe(false);
  });
});
