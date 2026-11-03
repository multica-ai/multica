import { AppState, Platform } from "react-native";
import * as Device from "expo-device";
import Constants from "expo-constants";
import { mobileTiming } from "@/data/ai-sessions/timing";
import type { LogLevel } from "./log-capture";
import { logCapture, setLogRuntime } from "./runtime";

interface ErrorUtilsLike {
  getGlobalHandler(): (error: unknown, isFatal?: boolean) => void;
  setGlobalHandler(handler: (error: unknown, isFatal?: boolean) => void): void;
}
interface HermesLike {
  enablePromiseRejectionTracker?(options: { allRejections: boolean; onUnhandled(id: number, error: unknown): void }): void;
}
const runtimeGlobals = globalThis as unknown as { ErrorUtils?: ErrorUtilsLike; HermesInternal?: HermesLike };

const CONSOLE_LEVELS = { debug: "debug", log: "info", info: "info", warn: "warning", error: "error" } as const satisfies Record<string, LogLevel>;

for (const [method, level] of Object.entries(CONSOLE_LEVELS) as [keyof typeof CONSOLE_LEVELS, LogLevel][]) {
  const original = console[method].bind(console);
  console[method] = (...args: unknown[]) => {
    logCapture.log(level, args);
    original(...args);
  };
}

const errors = runtimeGlobals.ErrorUtils;
if (errors) {
  const previous = errors.getGlobalHandler();
  errors.setGlobalHandler((error, isFatal) => {
    logCapture.log("error", [isFatal ? "[app] fatal error" : "[app] uncaught error", error]);
    previous(error, isFatal);
  });
}

// React Native only tracks these in development, where it reports them itself.
if (!__DEV__) {
  runtimeGlobals.HermesInternal?.enablePromiseRejectionTracker?.({
    allRejections: true,
    onUnhandled: (_id, error) => { logCapture.log("error", ["[app] unhandled rejection", error]); },
  });
}

mobileTiming.retain = (record) => { logCapture.mark(record); };

AppState.addEventListener("change", (state) => {
  if (state !== "active") logCapture.flush();
});

setLogRuntime({
  appVersion: Constants.expoConfig?.version ?? null,
  buildNumber: Constants.expoConfig?.ios?.buildNumber ?? null,
  platform: Platform.OS,
  osVersion: Device.osVersion,
  model: Device.modelName,
});
