// @vitest-environment node
import { describe, expect, it } from "vitest";
import { CLIENT_PLATFORM, CLIENT_VERSION, clientIdentityHeaders } from "./client-identity";

describe("clientIdentityHeaders", () => {
  it("reports android on Android JSON and upload requests", () => {
    expect(clientIdentityHeaders("android")).toEqual({
      "X-Client-Platform": CLIENT_PLATFORM,
      "X-Client-OS": "android",
      "X-Client-Version": CLIENT_VERSION,
    });
  });

  it("keeps ios when the runtime platform is iOS", () => {
    expect(clientIdentityHeaders("ios")).toEqual({
      "X-Client-Platform": CLIENT_PLATFORM,
      "X-Client-OS": "ios",
      "X-Client-Version": CLIENT_VERSION,
    });
  });
});
