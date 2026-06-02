import { LoginPage } from "@wallts/views/auth";
import { DragStrip } from "@wallts/views/platform";
import { WalltsIcon } from "@wallts/ui/components/common/wallts-icon";

export function DesktopLoginPage() {
  return (
    <div className="flex h-screen flex-col">
      <DragStrip />
      <LoginPage
        logo={<WalltsIcon bordered size="lg" />}
        onSuccess={() => {
          // Auth store update triggers AppContent re-render → shows DesktopShell.
          // Initial workspace navigation happens in routes.tsx via IndexRedirect.
        }}
      />
    </div>
  );
}
