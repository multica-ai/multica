/* global process, require */
const app = require("./app.json");

const googleIosClientId = process.env.GOOGLE_IOS_CLIENT_ID || "";
const googleIosUrlScheme = process.env.GOOGLE_IOS_URL_SCHEME || "";
const getuiAppId =
  process.env.GETUI_APPID || app.expo.extra.getuiAppId || "zopkAIG3P07bN78Q5CHck8";

export default {
  ...app.expo,
  extra: {
    ...app.expo.extra,
    apiBaseUrl: process.env.EXPO_PUBLIC_API_BASE_URL || app.expo.extra.apiBaseUrl,
    getuiAppId,
    googleIosClientId,
    webBaseUrl: process.env.EXPO_PUBLIC_WEB_BASE_URL || app.expo.extra.apiBaseUrl,
    wsUrl: process.env.EXPO_PUBLIC_WS_URL || app.expo.extra.wsUrl,
  },
  plugins: [
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
    ...(googleIosUrlScheme
      ? [
          [
            "@react-native-google-signin/google-signin",
            { iosUrlScheme: googleIosUrlScheme },
          ],
        ]
      : []),
  ],
};
