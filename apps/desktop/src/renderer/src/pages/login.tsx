import { LoginPage } from "@multica/views/auth";
import { DragStrip } from "@multica/views/platform";
import { MulticaIcon } from "@multica/ui/components/common/multica-icon";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@multica/ui/components/ui/dialog";
import { ServerPicker } from "../components/server-picker";

function requireRuntimeAppUrl(): string {
  const runtimeConfig = window.desktopAPI.runtimeConfig;
  if (!runtimeConfig.ok) {
    throw new Error(
      "Invariant violated: DesktopLoginPage rendered before App accepted runtime config",
    );
  }
  return runtimeConfig.config.appUrl;
}

// FIR-2037: the server the app talks to must be switchable BEFORE login —
// otherwise a wrong/unreachable server traps the user (the Settings → Server
// tab is behind auth). Show the current API host plus a "Change server" dialog
// on the login screen.
function ServerSwitcher() {
  const runtimeConfig = window.desktopAPI.runtimeConfig;
  const apiUrl = runtimeConfig.ok ? runtimeConfig.config.apiUrl : "";

  return (
    <Dialog>
      <div className="flex items-center justify-center gap-1.5 text-xs text-muted-foreground">
        <span className="font-mono break-all">{apiUrl || "no server set"}</span>
        <span>·</span>
        <DialogTrigger
          render={
            <button
              type="button"
              className="underline underline-offset-2 hover:text-foreground"
              style={{ WebkitAppRegion: "no-drag" } as React.CSSProperties}
            />
          }
        >
          Change server
        </DialogTrigger>
      </div>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Server</DialogTitle>
          <DialogDescription>
            Choose which Multica server this app connects to. Saving restarts the
            app.
          </DialogDescription>
        </DialogHeader>
        <ServerPicker />
      </DialogContent>
    </Dialog>
  );
}

export function DesktopLoginPage() {
  const webUrl = requireRuntimeAppUrl();
  const handleGoogleLogin = () => {
    // Open web login page in the default browser with platform=desktop flag.
    // The web callback will redirect back via multica:// deep link with the token.
    window.desktopAPI.openExternal(`${webUrl}/login?platform=desktop`);
  };

  return (
    <div className="flex h-screen flex-col">
      <DragStrip />
      <LoginPage
        logo={<MulticaIcon bordered size="lg" />}
        onSuccess={() => {
          // Auth store update triggers AppContent re-render → shows DesktopShell.
          // Initial workspace navigation happens in routes.tsx via IndexRedirect.
        }}
        onGoogleLogin={handleGoogleLogin}
      />
      <div className="pb-6 px-4">
        <ServerSwitcher />
      </div>
    </div>
  );
}
