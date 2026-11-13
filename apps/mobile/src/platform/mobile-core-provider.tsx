import type { ReactNode } from "react";
import { getLocales } from "expo-localization";
import { CoreProvider } from "@multica/core/platform";
import { subscribeAppActivity } from "./app-lifecycle";
import { endpoints } from "./env";
import { identity } from "./identity";
import { resolveLocale } from "./locale";
import { handleWorkspaceLost, selectWorkspace } from "./workspace";

const locale = resolveLocale(getLocales());
const resources = { [locale]: {} };

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
      locale={locale}
      resources={resources}
      onLogout={forgetWorkspace}
      subscribeAppActivity={subscribeAppActivity}
      onWorkspaceLost={handleWorkspaceLost}
    >
      {children}
    </CoreProvider>
  );
}
