import { LoginPage, buildOAuthProviderButtons } from "@multica/views/auth";
import { DragStrip } from "@multica/views/platform";
import { MulticaIcon } from "@multica/ui/components/common/multica-icon";
import { useConfigStore } from "@multica/core/config";

const WEB_URL = import.meta.env.VITE_APP_URL || "http://localhost:3000";

export function DesktopLoginPage() {
  const oauthProviders = useConfigStore((s) => s.oauthProviders);

  // Desktop hands off to the browser; token returns via multica:// deep link.
  const openWebLogin = () => {
    window.desktopAPI.openExternal(`${WEB_URL}/login?platform=desktop`);
  };

  const providers = buildOAuthProviderButtons(oauthProviders, openWebLogin);

  return (
    <div className="flex h-screen flex-col">
      <DragStrip />
      <LoginPage
        logo={<MulticaIcon bordered size="lg" />}
        onSuccess={() => {
          // Auth store update triggers AppContent re-render → shows DesktopShell.
          // Initial workspace navigation happens in routes.tsx via IndexRedirect.
        }}
        providers={providers}
      />
    </div>
  );
}
