import Constants from "expo-constants";

type ExtraConfig = {
  apiBaseUrl?: string;
  appLinkHosts?: string[];
  webBaseUrl?: string;
  wsUrl?: string;
};

const extra = (Constants.expoConfig?.extra ?? {}) as ExtraConfig;

export const MOBILE_ENV = {
  apiBaseUrl: extra.apiBaseUrl || "http://localhost:8080",
  appLinkHosts: Array.isArray(extra.appLinkHosts) ? extra.appLinkHosts : [],
  webBaseUrl: extra.webBaseUrl || extra.apiBaseUrl || "http://localhost:8080",
  wsUrl: extra.wsUrl || "ws://localhost:8080/ws",
  appScheme: "wujieai-multicam",
} as const;

export function getMobileIssueLinkBaseUrls(): string[] {
  return [
    MOBILE_ENV.webBaseUrl,
    MOBILE_ENV.apiBaseUrl,
    ...MOBILE_ENV.appLinkHosts.map((host) => `https://${host}`),
  ];
}
