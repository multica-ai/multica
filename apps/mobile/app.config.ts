import type { ExpoConfig, ConfigContext } from "expo/config";

const LOCAL_NETWORK_USAGE_DESCRIPTION =
  "Allow Multica to connect to your self-hosted server on the local network.";

type IosInfoPlist = {
  NSAppTransportSecurity: {
    NSAllowsLocalNetworking: true;
    NSExceptionDomains?: Record<
      string,
      {
        NSExceptionAllowsInsecureHTTPLoads: boolean;
        NSIncludesSubdomains: boolean;
      }
    >;
  };
  NSLocalNetworkUsageDescription?: string;
};

type NativeNetworkConfig = {
  androidUsesCleartextTraffic: boolean;
  iosInfoPlist?: IosInfoPlist;
};

function normalizeHostname(rawHostname: string): string {
  return rawHostname
    .toLowerCase()
    .replace(/^\[/, "")
    .replace(/\]$/, "")
    .replace(/\.$/, "");
}

function isIpAddress(hostname: string): boolean {
  const parts = hostname.split(".").map(Number);
  return (
    hostname.includes(":") ||
    (parts.length === 4 &&
      parts.every(
        (part) => Number.isInteger(part) && part >= 0 && part <= 255,
      ))
  );
}

function isLocalName(hostname: string): boolean {
  return (
    hostname === "localhost" ||
    hostname.endsWith(".localhost") ||
    hostname.endsWith(".local") ||
    !hostname.includes(".")
  );
}

/**
 * Generates native transport exceptions only when the configured API uses
 * cleartext HTTP. HTTPS builds keep the platform security defaults intact.
 */
export function getNativeNetworkConfig(
  apiUrl: string | undefined,
): NativeNetworkConfig {
  let url: URL;
  try {
    url = new URL(apiUrl ?? "");
  } catch {
    return { androidUsesCleartextTraffic: false };
  }

  if (url.protocol !== "http:") {
    return { androidUsesCleartextTraffic: false };
  }

  const hostname = normalizeHostname(url.hostname);
  const domainExceptions = isIpAddress(hostname) || isLocalName(hostname)
    ? {}
    : {
        NSExceptionDomains: {
          [hostname]: {
            NSExceptionAllowsInsecureHTTPLoads: true,
            NSIncludesSubdomains: false,
          },
        },
      };

  return {
    androidUsesCleartextTraffic: true,
    iosInfoPlist: {
      NSAppTransportSecurity: {
        NSAllowsLocalNetworking: true,
        ...domainExceptions,
      },
      NSLocalNetworkUsageDescription: LOCAL_NETWORK_USAGE_DESCRIPTION,
    },
  };
}

/**
 * Dynamic Expo config — replaces app.json so we can read APP_ENV at runtime
 * and switch bundleIdentifier / display name for dev / staging / production.
 *
 * APP_ENV is set by package.json scripts:
 *   - dev          → APP_ENV unset (treated as "development")
 *   - dev:staging  → APP_ENV=staging
 *   - dev:prod     → APP_ENV=production (rare; usually only for EAS build)
 */
export default ({ config }: ConfigContext): ExpoConfig => {
  const env = process.env.APP_ENV ?? "development";
  const isProd = env === "production";
  const isStaging = env === "staging";
  const nativeNetworkConfig = getNativeNetworkConfig(
    process.env.EXPO_PUBLIC_API_URL,
  );

  return {
    ...config,
    name: isProd
      ? "Multica"
      : isStaging
        ? "Multica (Staging)"
        : "Multica (Dev)",
    slug: "multica-mobile",
    version: "0.1.0",
    orientation: "portrait",
    userInterfaceStyle: "automatic",
    scheme: "multica",
    // 1024x1024 source shared with the desktop client
    // (apps/desktop/build/icon.png). Expo prebuild generates every required
    // iOS icon size from this single PNG.
    icon: "./assets/icon.png",
    ios: {
      ...config.ios,
      supportsTablet: false,
      // Pins DEVELOPMENT_TEAM on every prebuild. Leaving it unset is the normal
      // path — `expo run:ios` then resolves a signing identity from the Keychain
      // itself, which is right when the Apple ID owns exactly one team. With
      // several (a personal team plus an employer's) it takes the *first*
      // identity found whenever the terminal is non-interactive, writes that
      // choice into the generated ios/, and never clears it again: prebuild only
      // writes DEVELOPMENT_TEAM when a value is present, so a project pinned to
      // the wrong team stays wrong until ios/ is deleted. Setting this re-applies
      // the intended team on every `scripts/ios-run.sh` run, which also repairs
      // an already-mispinned checkout.
      appleTeamId: process.env.EXPO_APPLE_TEAM_ID,
      // Per-variant bundle id overrides exist for one reason: an Apple ID
      // can only sign bundle prefixes it owns, so contributors not on the
      // Multica Apple Developer team (and external users self-building a
      // personal copy against production) need to swap to a reverse-domain
      // they control. Each variant has its own `_<VARIANT>` suffix and is
      // only read inside that variant's branch — a generic
      // `EXPO_BUNDLE_IDENTIFIER` would leak across variants (Expo CLI
      // auto-loads `.env.<mode>.local` regardless of APP_ENV) and collapse
      // dev / staging / prod onto a single id.
      bundleIdentifier: isProd
        ? (process.env.EXPO_BUNDLE_IDENTIFIER_PROD ?? "ai.multica.mobile")
        : isStaging
          ? "ai.multica.mobile.staging"
          : (process.env.EXPO_BUNDLE_IDENTIFIER_DEV ?? "ai.multica.mobile.dev"),
      ...(nativeNetworkConfig.iosInfoPlist
        ? {
            infoPlist: {
              ...config.ios?.infoPlist,
              ...nativeNetworkConfig.iosInfoPlist,
            },
          }
        : {}),
    },
    plugins: [
      "expo-router",
      "expo-secure-store",
      "@react-native-community/datetimepicker",
      "react-native-enriched-markdown",
      [
        "expo-image-picker",
        {
          // iOS NSPhotoLibraryUsageDescription. Without this string in
          // Info.plist, calling launchImageLibraryAsync hard-crashes on
          // iOS 14+. Camera + microphone are disabled — we only ever read
          // from the existing photo library.
          photosPermission:
            "Allow Multica to access your photos to attach images to issues and comments.",
          cameraPermission: false,
          microphonePermission: false,
        },
      ],
      [
        "expo-build-properties",
        {
          android: {
            usesCleartextTraffic:
              nativeNetworkConfig.androidUsesCleartextTraffic,
          },
          ios: {
            buildReactNativeFromSource: true,
          },
        },
      ],
    ],
    extra: { APP_ENV: env },
  };
};
