import Constants from "expo-constants";
import { Platform } from "react-native";
import type { ClientIdentity } from "@multica/core/platform";

export const identity: ClientIdentity = {
  platform: "mobile",
  version: Constants.expoConfig?.version ?? "0.0.0",
  os: Platform.OS,
};
