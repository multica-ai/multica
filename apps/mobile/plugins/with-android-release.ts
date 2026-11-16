import { withAppBuildGradle, type ConfigPlugin } from "expo/config-plugins";

/** Expo's template signs Release with the debug key; sign externally instead. */
const withAndroidRelease: ConfigPlugin = (config) =>
  withAppBuildGradle(config, (mod) => {
    const contents = mod.modResults.contents;
    const releaseSigning = /(release \{[\s\S]*?)signingConfig signingConfigs\.debug/;
    if (releaseSigning.test(contents)) {
      mod.modResults.contents = contents.replace(releaseSigning, "$1signingConfig null");
    } else if (!/release \{[\s\S]*?signingConfig null/.test(contents)) {
      throw new Error("Cannot disable the template's Release signing; review android/app/build.gradle");
    }
    return mod;
  });

export default withAndroidRelease;
