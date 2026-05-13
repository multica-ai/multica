import { LoginPage } from "@multica/views/auth";
import { DragStrip } from "@multica/views/platform";
import { MulticaIcon } from "@multica/ui/components/common/multica-icon";
import { useConfigStore } from "@multica/core/config";

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
  const larkAuthEnabled = useConfigStore((state) => state.larkAuthEnabled);
  const handleGoogleLogin = () => {
    // Open web login page in the default browser with platform=desktop flag.
    // The web callback will redirect back via multica:// deep link with the token.
    window.desktopAPI.openExternal(
      `${webUrl}/login?platform=desktop`,
    );
  };
  const handleLarkLogin = () => {
    window.desktopAPI.openExternal(
      `${webUrl}/login?platform=desktop`,
    );
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
        onGoogleLogin={larkAuthEnabled ? undefined : handleGoogleLogin}
        onLarkLogin={larkAuthEnabled ? handleLarkLogin : undefined}
        showEmailLogin={!larkAuthEnabled}
      />
    </div>
  );
}
