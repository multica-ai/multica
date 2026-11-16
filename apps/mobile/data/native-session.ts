import * as ReactNative from "react-native";

// RN exports Networking on both platforms, but omits it from its legacy
// TypeScript surface. Use the public export, not a private module path.
const { Networking } = ReactNative as typeof ReactNative & {
  Networking: { clearCookies: (callback: (cleared: boolean) => void) => void };
};

/** Old clients accepted the login response's browser cookie. It is scoped to
 * a host, not a port, so it must not survive a native instance switch. */
export function clearNativeSessionCookies(): Promise<void> {
  return new Promise((resolve) => Networking.clearCookies(() => resolve()));
}

let initialization: Promise<void> | null = null;

export function initializeNativeSession(): Promise<void> {
  if (!initialization) {
    initialization = clearNativeSessionCookies().catch((error: unknown) => {
      initialization = null;
      throw error;
    });
  }
  return initialization;
}
