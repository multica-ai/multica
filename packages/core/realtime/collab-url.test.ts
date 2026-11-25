import { describe, expect, it } from "vitest";
import { collabUrlFromWsUrl } from "./provider";

describe("collabUrlFromWsUrl", () => {
  it("serves co-editing next to realtime", () => {
    expect(collabUrlFromWsUrl("ws://localhost:8080/ws")).toBe("ws://localhost:8080/collab");
    expect(collabUrlFromWsUrl("wss://api.example.com/base/ws?x=1")).toBe("wss://api.example.com/base/collab");
  });

  it("resolves a same-origin URL", () => {
    expect(collabUrlFromWsUrl("https://app.example.com/ws")).toBe("wss://app.example.com/collab");
  });
});
