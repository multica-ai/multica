import type { ReactNode } from "react";
import { CoreProvider } from "@multica/core/platform";
import { subscribeAppActivity } from "./app-lifecycle";
import { appLocale } from "./app-locale";
import { endpoints } from "./env";
import { identity } from "./identity";
import { handleWorkspaceLost, selectWorkspace } from "./workspace";

const resources = { [appLocale]: {} };

const forgetWorkspace = () => selectWorkspace(null);

/**
 * Core in token mode: the session token lives in the Keychain through the
 * storage backend installed at startup, and every other piece of client state
 * (queries, realtime, auth, persisted stores) is core's own.
 *
 * No locale adapter: core's switcher reloads the page to apply a language.
 */
export function MobileCoreProvider({ children }: { children: ReactNode }) {
  return (
    <CoreProvider
      apiBaseUrl={endpoints.apiUrl}
      wsUrl={endpoints.wsUrl}
      identity={identity}
      locale={appLocale}
      resources={resources}
      onLogout={forgetWorkspace}
      subscribeAppActivity={subscribeAppActivity}
      onWorkspaceLost={handleWorkspaceLost}
    >
      {children}
    </CoreProvider>
  );
}
