import { LoginPage } from "@multica/views/auth";
import { DragStrip } from "@multica/views/platform";
import { MulticaIcon } from "@multica/ui/components/common/multica-icon";
import { DesktopServerSwitcher } from "../components/server-switcher";

function requireRuntimeAppUrl(): string {
  const runtimeConfig = window.desktopAPI.runtimeConfig;
  if (!runtimeConfig.ok) {
    throw new Error(
      "Invariant violated: DesktopLoginPage rendered before App accepted runtime config",
    );
  }
  return runtimeConfig.config.appUrl;
}

export function DesktopLoginPage() {
  const webUrl = requireRuntimeAppUrl();
  const handleGoogleLogin = () => {
    // Open web login page in the default browser with platform=desktop flag.
    // The web callback will redirect back via multica:// deep link with the token.
    window.desktopAPI.openExternal(
      `${webUrl}/login?platform=desktop`,
    );
  };

  return (
    <div className="flex h-screen flex-col">
      <DragStrip />
      <div className="flex flex-1 flex-col">
        <div className="flex justify-center pt-6">
          <DesktopServerSwitcher />
        </div>
        <div className="flex flex-1 flex-col">
          <LoginPage
            logo={<MulticaIcon bordered size="lg" />}
            onSuccess={() => {
              // Auth store update triggers AppContent re-render → shows DesktopShell.
              // Initial workspace navigation happens in routes.tsx via IndexRedirect.
            }}
            onGoogleLogin={handleGoogleLogin}
          />
        </div>
      </div>
    </div>
  );
}
