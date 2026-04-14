import { LoginPage } from "@multica/views/auth";
import { MulticaIcon } from "@multica/ui/components/common/multica-icon";

const WEB_URL = import.meta.env.VITE_WEB_URL || "http://localhost:3000";

export function DesktopLoginPage() {
  const lastWorkspaceId = localStorage.getItem("multica_workspace_id");

  const handleGoogleLogin = () => {
    // Open web login page in the default browser with platform=desktop flag.
    // The web callback will redirect back via multica:// deep link with the token.
    window.desktopAPI.openExternal(
      `${WEB_URL}/login?platform=desktop`,
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
        lastWorkspaceId={lastWorkspaceId}
        onSuccess={() => {
          // Auth store update triggers AppContent re-render → shows DesktopShell
        }}
        onGoogleLogin={handleGoogleLogin}
      />
    </div>
  );
}
