// FIR-3091 slice 5: the web_fetch host list moves into the unified Permissions
// screen. This pins the two-flag coordination so the editor can never appear in
// two places (standalone tab AND Permissions) or vanish from both. The section
// itself is rendered by cerebro-tool-policy's WorkspacePermissionsTab under the
// same cerebro_web_fetch_permissions flag; here we lock the tab-suppression half.
import { describe, expect, it, vi, beforeEach } from "vitest";
import { renderHook } from "@testing-library/react";

const mockUseFeatureFlag = vi.hoisted(() => vi.fn());
vi.mock("@multica/cerebro-feature-flags", () => ({
  useFeatureFlag: (key: string) => mockUseFeatureFlag(key),
}));

import { useCerebroWebFetchPolicySettingsTabs } from "./workspace-web-fetch-policy-settings-tab";

function withFlags(flags: Record<string, boolean>) {
  mockUseFeatureFlag.mockImplementation((key: string) => flags[key] ?? false);
}

describe("useCerebroWebFetchPolicySettingsTabs", () => {
  beforeEach(() => {
    mockUseFeatureFlag.mockReset();
  });

  it("shows the standalone Web fetch tab when the feature is on and not relocated", () => {
    withFlags({ cerebro_web_fetch_policy: true, cerebro_web_fetch_permissions: false });
    const { result } = renderHook(() => useCerebroWebFetchPolicySettingsTabs());
    expect(result.current.map((t) => t.value)).toEqual(["web-fetch"]);
  });

  it("suppresses the standalone tab once the list is relocated into Permissions", () => {
    withFlags({ cerebro_web_fetch_policy: true, cerebro_web_fetch_permissions: true });
    const { result } = renderHook(() => useCerebroWebFetchPolicySettingsTabs());
    expect(result.current).toEqual([]);
  });

  it("shows nothing when the web_fetch feature itself is off", () => {
    withFlags({ cerebro_web_fetch_policy: false, cerebro_web_fetch_permissions: false });
    const { result } = renderHook(() => useCerebroWebFetchPolicySettingsTabs());
    expect(result.current).toEqual([]);
  });
});
