const ANDROID_PACKAGE_NAME = "com.wujieai.multica";
const IOS_BUNDLE_ID = "com.wujieai.multica";

export type AndroidAssetLink = {
  relation: string[];
  target: {
    namespace: "android_app";
    package_name: string;
    sha256_cert_fingerprints: string[];
  };
};

export type AppleAppSiteAssociation = {
  applinks: {
    details: Array<{
      appIDs: string[];
      components: Array<{ "/": string }>;
    }>;
  };
};

export function buildAndroidAssetLinks(
  value = process.env.MULTICA_ANDROID_SHA256_CERT_FINGERPRINTS,
): AndroidAssetLink[] {
  const fingerprints = parseList(value);
  if (fingerprints.length === 0) return [];

  return [
    {
      relation: ["delegate_permission/common.handle_all_urls"],
      target: {
        namespace: "android_app",
        package_name: ANDROID_PACKAGE_NAME,
        sha256_cert_fingerprints: fingerprints,
      },
    },
  ];
}

export function hasAndroidAssetLinks(
  value = process.env.MULTICA_ANDROID_SHA256_CERT_FINGERPRINTS,
): boolean {
  return parseList(value).length > 0;
}

export function buildAppleAppSiteAssociation(
  value = process.env.MULTICA_IOS_APP_IDS || process.env.MULTICA_APPLE_TEAM_ID,
): AppleAppSiteAssociation {
  return {
    applinks: {
      details: [
        {
          appIDs: parseAppleAppIds(value),
          components: [{ "/": "/*/issues/*" }],
        },
      ],
    },
  };
}

export function hasAppleAppIds(
  value = process.env.MULTICA_IOS_APP_IDS || process.env.MULTICA_APPLE_TEAM_ID,
): boolean {
  return parseAppleAppIds(value).length > 0;
}

function parseAppleAppIds(value?: string): string[] {
  return parseList(value).map((item) => (
    item.includes(".") ? item : `${item}.${IOS_BUNDLE_ID}`
  ));
}

function parseList(value?: string): string[] {
  if (!value) return [];

  return Array.from(new Set(
    value
      .split(/[,\n]/)
      .map((item) => item.trim())
      .filter(Boolean),
  ));
}
