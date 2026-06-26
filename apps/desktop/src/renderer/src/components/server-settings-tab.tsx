import { ServerPicker } from "./server-picker";

// FIR-2037: Settings → Server. Lets the user point the desktop app at
// Production / Staging / a custom backend without editing ~/.multica/desktop.json
// by hand. The picker itself is shared with the login screen (ServerPicker).

export function ServerSettingsTab() {
  return (
    <div>
      <h2 className="text-lg font-semibold">Server</h2>
      <p className="text-sm text-muted-foreground mt-1">
        Choose which Multica server this app connects to. Changing it signs you
        out of the current server and restarts the app.
      </p>

      <div className="mt-6">
        <ServerPicker />
      </div>
    </div>
  );
}
