import { createRoot } from "react-dom/client";
import { I18nProvider } from "@multica/core/i18n/react";
import { RESOURCES } from "@multica/views/locales";
import { DaemonSettingsTab } from "../src/renderer/src/components/daemon-settings-tab";

// Screenshot input only. All values and Electron responses are synthetic.
Object.defineProperty(window, "daemonAPI", {
  value: {
    getPrefs: async () => ({ autoStart: false, autoStop: false }),
    isCliInstalled: async () => true,
    getStatus: async () => ({ state: "stopped", profile: "desktop-fixture" }),
    onStatusChange: () => () => {},
    exportDiagnostics: async () => ({ status: "saved", partial: true }),
  },
});

const locale =
  new URLSearchParams(window.location.search).get("locale") === "zh-Hans"
    ? "zh-Hans"
    : "en";
createRoot(document.getElementById("root")!).render(
  <I18nProvider locale={locale} resources={RESOURCES}>
    <main className="mx-auto max-w-4xl p-8">
      <p className="mb-6 text-caption text-muted-foreground">
        Synthetic Desktop diagnostics fixture · Partial export result · Electron
        save dialog boundary stubbed
      </p>
      <DaemonSettingsTab />
    </main>
  </I18nProvider>,
);
