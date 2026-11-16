// @vitest-environment node
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { createRequire } from "node:module";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ConfigContext } from "expo/config";
import createConfig from "./app.config";

afterEach(() => vi.unstubAllEnvs());

describe("iOS scene lifecycle configuration", () => {
  it("resolves native presenters from the foregrounded scene", () => {
    const requireFromTest = createRequire(import.meta.url);
    const requireFromExpo = createRequire(requireFromTest.resolve("expo/package.json"));
    const modulesCoreRoot = dirname(
      requireFromExpo.resolve("expo-modules-core/package.json"),
    );
    const utilities = readFileSync(
      join(modulesCoreRoot, "ios", "Utilities", "Utilities.swift"),
      "utf8",
    );

    expect(utilities).toContain("return SceneGeometry.keyWindow()");
    expect(utilities).not.toContain(
      "UIApplication.shared.keyWindow?.rootViewController",
    );
  });

  it.each([
    [undefined, "Multica (Dev)", "ai.multica.mobile.dev"],
    ["development", "Multica (Dev)", "ai.multica.mobile.dev"],
    ["staging", "Multica (Staging)", "ai.multica.mobile.staging"],
    ["production", "Multica", "ai.multica.mobile"],
  ])("enables scene support for %s without changing app identity", (env, name, bundleIdentifier) => {
    vi.stubEnv("APP_ENV", env);
    vi.stubEnv("EXPO_BUNDLE_IDENTIFIER_DEV", undefined);
    vi.stubEnv("EXPO_BUNDLE_IDENTIFIER_STAGING", undefined);
    vi.stubEnv("EXPO_BUNDLE_IDENTIFIER_PROD", undefined);
    vi.stubEnv("EXPO_ANDROID_PACKAGE_DEV", undefined);
    vi.stubEnv("EXPO_ANDROID_PACKAGE_STAGING", undefined);
    vi.stubEnv("EXPO_ANDROID_PACKAGE_PROD", undefined);
    const config = createConfig({ config: {} } as ConfigContext);

    expect(config.name).toBe(name);
    expect(config.ios?.bundleIdentifier).toBe(bundleIdentifier);
    expect(config.android?.package).toBe(bundleIdentifier);
    expect(config.scheme).toBe("multica");
    expect(config.plugins).toContainEqual([
      "expo-build-properties",
      {
        android: { minSdkVersion: 24, usesCleartextTraffic: false },
        ios: { buildReactNativeFromSource: true, enableSceneSupport: true },
      },
    ]);
  });

  it.each([
    ["development", "com.example.multica.dev"],
    ["staging", "com.example.multica.staging"],
    ["production", "com.example.multica"],
  ])("preserves signing overrides for %s", (env, bundleIdentifier) => {
    vi.stubEnv("APP_ENV", env);
    vi.stubEnv("EXPO_BUNDLE_IDENTIFIER_DEV", "com.example.multica.dev");
    vi.stubEnv("EXPO_BUNDLE_IDENTIFIER_STAGING", "com.example.multica.staging");
    vi.stubEnv("EXPO_BUNDLE_IDENTIFIER_PROD", "com.example.multica");
    vi.stubEnv("EXPO_APPLE_TEAM_ID", "ABCDE12345");

    const config = createConfig({ config: {} } as ConfigContext);
    expect(config.ios).toMatchObject({ bundleIdentifier, appleTeamId: "ABCDE12345" });
    expect(config.scheme).toBe("multica");
  });
});

describe("Android build identity", () => {
  it("keeps package overrides isolated between variants", () => {
    vi.stubEnv("EXPO_ANDROID_PACKAGE_PROD", "com.example.multica");
    vi.stubEnv("APP_ENV", "staging");
    expect(createConfig({ config: {} } as ConfigContext).android?.package).toBe("ai.multica.mobile.staging");
    vi.stubEnv("APP_ENV", "production");
    vi.stubEnv("EXPO_ANDROID_VERSION_CODE", "17");
    expect(createConfig({ config: {} } as ConfigContext).android).toMatchObject({
      package: "com.example.multica", versionCode: 17, softwareKeyboardLayoutMode: "resize",
    });
  });

  it.each(["0", "-1", "1.5", "NaN", "2100000001"])("rejects invalid versionCode %s", (code) => {
    vi.stubEnv("EXPO_ANDROID_VERSION_CODE", code);
    expect(() => createConfig({ config: {} } as ConfigContext)).toThrow("EXPO_ANDROID_VERSION_CODE");
  });

  it("requires an explicit cleartext opt-in and rejects unknown variants", () => {
    vi.stubEnv("EXPO_ANDROID_ALLOW_CLEARTEXT", "1");
    expect(createConfig({ config: {} } as ConfigContext).plugins).toContainEqual([
      "expo-build-properties",
      {
        android: { minSdkVersion: 24, usesCleartextTraffic: true },
        ios: { buildReactNativeFromSource: true, enableSceneSupport: true },
      },
    ]);
    vi.stubEnv("APP_ENV", "prodution");
    expect(() => createConfig({ config: {} } as ConfigContext)).toThrow("Unsupported APP_ENV");
  });
});
