/* global process, require */
const app = require("./app.json");

const getuiAppId =
  process.env.GETUI_APPID || app.expo.extra.getuiAppId || "zopkAIG3P07bN78Q5CHck8";
const appLinkHosts = parseAppLinkHosts(
  process.env.EXPO_PUBLIC_APP_LINK_HOSTS ||
    app.expo.extra.appLinkHosts ||
    "multica.wujieai.com",
);

function parseAppLinkHosts(value) {
  return Array.from(new Set(
    String(value)
      .split(",")
      .map((item) => item.trim())
      .filter(Boolean)
      .map((item) => {
        try {
          return new URL(item).hostname;
        } catch {
          return item;
        }
      })
      .filter((item) => /^[a-z0-9.-]+$/i.test(item)),
  ));
}

export default {
  ...app.expo,
  android: {
    ...app.expo.android,
    intentFilters: appLinkHosts.map((host) => ({
      action: "VIEW",
      autoVerify: true,
      category: ["BROWSABLE", "DEFAULT"],
      data: [
        {
          scheme: "https",
          host,
          pathPrefix: "/",
        },
      ],
    })),
  },
  extra: {
    ...app.expo.extra,
    appLinkHosts,
    apiBaseUrl: process.env.EXPO_PUBLIC_API_BASE_URL || app.expo.extra.apiBaseUrl,
    getuiAppId,
    webBaseUrl: process.env.EXPO_PUBLIC_WEB_BASE_URL || app.expo.extra.apiBaseUrl,
    wsUrl: process.env.EXPO_PUBLIC_WS_URL || app.expo.extra.wsUrl,
  },
  ios: {
    ...app.expo.ios,
    associatedDomains: appLinkHosts.map((host) => `applinks:${host}`),
  },
  plugins: [
    "expo-notifications",
    [
      "./plugins/with-multica-android-native.cjs",
      {
        getuiAppId,
      },
    ],
    [
      "expo-splash-screen",
      {
        backgroundColor: "#FFFFFF",
        image: "./assets/splash-icon-light-safe.png",
        imageWidth: 200,
        resizeMode: "contain",
        dark: {
          backgroundColor: "#000000",
          image: "./assets/splash-icon-dark-safe.png",
        },
      },
    ],
    ...(app.expo.plugins || []),
  ],
};
