import { describe, expect, it } from "vitest";
import {
  OFFICIAL_MULTICA_API_URL,
  isOfficialMulticaApiUrl,
  isSelfHostedApiBaseUrl,
  normalizeApiBaseUrl,
  shouldShowSourceChannelReporting,
} from "./source-reporting";

describe("source channel API-url detection", () => {
  it("treats only the official API URL as official", () => {
    expect(isOfficialMulticaApiUrl(OFFICIAL_MULTICA_API_URL)).toBe(true);
    expect(isOfficialMulticaApiUrl("https://api.multica.ai/")).toBe(true);
    expect(isOfficialMulticaApiUrl("https://api.example.com")).toBe(false);
    expect(isOfficialMulticaApiUrl("https://multica.ai")).toBe(false);
    expect(isOfficialMulticaApiUrl("http://api.multica.ai")).toBe(false);
  });

  it("normalizes harmless URL drift before comparing", () => {
    expect(normalizeApiBaseUrl(" https://API.MULTICA.AI/ ")).toBe(
      OFFICIAL_MULTICA_API_URL,
    );
    expect(normalizeApiBaseUrl("https://api.multica.ai?x=1#hash")).toBe(
      OFFICIAL_MULTICA_API_URL,
    );
  });

  it("shows source reporting only for non-official API URLs", () => {
    expect(isSelfHostedApiBaseUrl(OFFICIAL_MULTICA_API_URL)).toBe(false);
    expect(isSelfHostedApiBaseUrl("https://api.customer.example")).toBe(true);
    expect(shouldShowSourceChannelReporting(OFFICIAL_MULTICA_API_URL)).toBe(
      false,
    );
    expect(shouldShowSourceChannelReporting("https://api.customer.example")).toBe(
      true,
    );
  });
});
