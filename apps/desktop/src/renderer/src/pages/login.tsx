import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { LoginPage } from "@multica/views/auth";
import { useT } from "@multica/views/i18n";
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { DragStrip } from "@multica/views/platform";
import { MulticaIcon } from "@multica/ui/components/common/multica-icon";
import { localDevTokenForApi } from "../platform/local-dev-auth";

function requireRuntimeConfig() {
  const runtimeConfig = window.desktopAPI.runtimeConfig;
  if (!runtimeConfig.ok) {
    throw new Error(
      "Invariant violated: DesktopLoginPage rendered before App accepted runtime config",
    );
  }
  return runtimeConfig.config;
}

function LocalProfileLoginLink({ token }: { token: string }) {
  const { t } = useT("auth");
  const queryClient = useQueryClient();
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState(false);

  const handleSkipLogin = async () => {
    if (loading) return;
    setLoading(true);
    setError(false);
    try {
      await useAuthStore.getState().loginWithToken(token);
      const workspaces = await api.listWorkspaces();
      queryClient.setQueryData(workspaceKeys.list(), workspaces);
    } catch {
      useAuthStore.getState().logout();
      setError(true);
      setLoading(false);
    }
  };

  return (
    <div className="flex flex-col items-center gap-1.5">
      <button
        type="button"
        onClick={handleSkipLogin}
        disabled={loading}
        className="text-caption text-muted-foreground underline-offset-4 transition-colors hover:text-foreground hover:underline disabled:cursor-not-allowed disabled:opacity-50"
      >
        {loading
          ? t(($) => $.desktop.local_login.loading)
          : t(($) => $.desktop.local_login.skip)}
      </button>
      {error && (
        <p className="text-caption text-destructive" role="alert">
          {t(($) => $.desktop.local_login.failed)}
        </p>
      )}
    </div>
  );
}

export function DesktopLoginPage() {
  const runtimeConfig = requireRuntimeConfig();
  const localProfileToken = import.meta.env.DEV
    ? localDevTokenForApi(
        runtimeConfig.apiUrl,
        import.meta.env.VITE_DESKTOP_DEV_AUTH_TOKEN,
      )
    : null;
  const handleGoogleLogin = () => {
    // Open web login page in the default browser with platform=desktop flag.
    // The web callback will redirect back via multica:// deep link with the token.
    window.desktopAPI.openExternal(
      `${runtimeConfig.appUrl}/login?platform=desktop`,
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
        onGoogleLogin={handleGoogleLogin}
        extra={
          localProfileToken ? (
            <LocalProfileLoginLink token={localProfileToken} />
          ) : undefined
        }
      />
    </div>
  );
}
