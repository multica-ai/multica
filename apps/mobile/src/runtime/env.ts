import Constants from "expo-constants";

type ExtraConfig = {
  apiBaseUrl?: string;
  wsUrl?: string;
};

const extra = (Constants.expoConfig?.extra ?? {}) as ExtraConfig;

export const MOBILE_ENV = {
  apiBaseUrl: extra.apiBaseUrl || "http://localhost:8080",
  wsUrl: extra.wsUrl || "ws://localhost:8080/ws",
  appScheme: "wujieai_multicam",
} as const;
