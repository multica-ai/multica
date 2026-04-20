import { LoginPage } from "@multica/views/auth";
import { MulticaIcon } from "@multica/ui/components/common/multica-icon";

const WEB_URL = import.meta.env.VITE_APP_URL || "http://localhost:3000";

export function DesktopLoginPage() {
  const handleGoogleLogin = () => {
    // Open the hosted login page in the default browser. The callback page
    // will redirect back via multica:// deep link with the token.
    window.desktopAPI.openExternal(
      `${WEB_URL}/login?platform=desktop&provider=google`,
    );
  };

  const handleFeishuLogin = () => {
    window.desktopAPI.openExternal(
      `${WEB_URL}/login?platform=desktop&provider=feishu`,
    );
  };

  return (
    <div className="flex h-screen flex-col">
      {/* Traffic light inset */}
      <div
        className="h-[38px] shrink-0"
        style={{ WebkitAppRegion: "drag" } as React.CSSProperties}
      />
      <LoginPage
        logo={<MulticaIcon bordered size="lg" />}
        onSuccess={() => {
          // Auth store update triggers AppContent re-render → shows DesktopShell.
          // Initial workspace navigation happens in routes.tsx via IndexRedirect.
        }}
        onGoogleLogin={handleGoogleLogin}
        onFeishuLogin={handleFeishuLogin}
      />
    </div>
  );
}
