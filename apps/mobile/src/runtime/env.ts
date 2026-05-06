import Constants from "expo-constants";

type ExtraConfig = {
  apiBaseUrl?: string;
  googleIosClientId?: string;
  webBaseUrl?: string;
  wsUrl?: string;
};

const extra = (Constants.expoConfig?.extra ?? {}) as ExtraConfig;

export const MOBILE_ENV = {
  apiBaseUrl: extra.apiBaseUrl || "http://localhost:8080",
  googleIosClientId: extra.googleIosClientId || "",
  webBaseUrl: extra.webBaseUrl || extra.apiBaseUrl || "http://localhost:8080",
  wsUrl: extra.wsUrl || "ws://localhost:8080/ws",
  appScheme: "wujieai-multicam",
} as const;
