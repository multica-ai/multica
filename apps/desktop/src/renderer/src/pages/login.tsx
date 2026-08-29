import { useState, type FormEvent } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { LoginPage } from "@multica/views/auth";
import { useT } from "@multica/views/i18n";
import { DragStrip } from "@multica/views/platform";
import { MulticaIcon } from "@multica/ui/components/common/multica-icon";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@multica/ui/components/ui/card";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { api, ApiError } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { useConfigStore } from "@multica/core/config";
import { workspaceKeys } from "@multica/core/workspace/queries";

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
  if (window.desktopAPI.appInfo.flavor === "lifeos") {
    return <DesktopLifeOSLoginPage />;
  }

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
      <LoginPage
        logo={<MulticaIcon bordered size="lg" />}
        onSuccess={() => {
          // Auth store update triggers AppContent re-render → shows DesktopShell.
          // Initial workspace navigation happens in routes.tsx via IndexRedirect.
        }}
        onGoogleLogin={handleGoogleLogin}
      />
    </div>
  );
}

function DesktopLifeOSLoginPage() {
  const { t } = useT("auth");
  const queryClient = useQueryClient();
  const localAuthConfigured = useConfigStore(
    (state) => state.localAuthConfigured,
  );
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [passwordConfirm, setPasswordConfirm] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");

  const handleSubmit = async (event: FormEvent) => {
    event.preventDefault();
    if (!localAuthConfigured && password !== passwordConfirm) {
      setError(t(($) => $.lifeos.password_mismatch));
      return;
    }

    setSubmitting(true);
    setError("");
    try {
      const session = localAuthConfigured
        ? await api.localLogin(username, password)
        : await api.localSetup(username, password);
      localStorage.setItem("multica_token", session.token);
      api.setToken(session.token);
      queryClient.setQueryData(workspaceKeys.list(), [session.workspace]);
      useAuthStore.setState({ user: session.user, isLoading: false });
    } catch (caught) {
      if (caught instanceof ApiError && caught.status === 429) {
        setError(t(($) => $.lifeos.too_many_attempts));
      } else {
        setError(
          t(($) =>
            localAuthConfigured
              ? $.lifeos.invalid_credentials
              : $.lifeos.setup_failed,
          ),
        );
      }
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="flex h-screen flex-col bg-app-shell">
      <DragStrip />
      <main className="flex min-h-0 flex-1 items-center justify-center px-4 pb-12">
        <Card className="w-full max-w-sm">
          <CardHeader className="text-center">
            <div className="mx-auto mb-3">
              <MulticaIcon bordered size="lg" />
            </div>
            <CardTitle className="text-2xl text-balance">
              {t(($) => $.lifeos.title)}
            </CardTitle>
            <CardDescription className="text-pretty">
              {t(($) =>
                localAuthConfigured
                  ? $.lifeos.login_description
                  : $.lifeos.setup_description,
              )}
            </CardDescription>
          </CardHeader>
          <CardContent>
            <form onSubmit={handleSubmit} className="space-y-4">
              <div className="space-y-2">
                <Label htmlFor="lifeos-desktop-username">
                  {t(($) => $.lifeos.username)}
                </Label>
                <Input
                  id="lifeos-desktop-username"
                  name="username"
                  value={username}
                  onChange={(event) => setUsername(event.target.value)}
                  autoComplete="username"
                  autoCapitalize="none"
                  spellCheck={false}
                  autoFocus
                  required
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="lifeos-desktop-password">
                  {t(($) => $.lifeos.password)}
                </Label>
                <Input
                  id="lifeos-desktop-password"
                  name="password"
                  type="password"
                  value={password}
                  onChange={(event) => setPassword(event.target.value)}
                  autoComplete={
                    localAuthConfigured ? "current-password" : "new-password"
                  }
                  required
                />
              </div>
              {!localAuthConfigured && (
                <>
                  <p className="text-xs leading-5 text-muted-foreground text-pretty">
                    {t(($) => $.lifeos.password_requirements)}
                  </p>
                  <div className="space-y-2">
                    <Label htmlFor="lifeos-desktop-password-confirm">
                      {t(($) => $.lifeos.password_confirm)}
                    </Label>
                    <Input
                      id="lifeos-desktop-password-confirm"
                      name="password-confirm"
                      type="password"
                      value={passwordConfirm}
                      onChange={(event) =>
                        setPasswordConfirm(event.target.value)
                      }
                      autoComplete="new-password"
                      required
                    />
                  </div>
                </>
              )}
              {error && (
                <p role="alert" className="text-sm text-destructive">
                  {error}
                </p>
              )}
              <Button
                type="submit"
                className="w-full"
                size="lg"
                disabled={
                  submitting ||
                  !username ||
                  !password ||
                  (!localAuthConfigured && !passwordConfirm)
                }
              >
                {submitting
                  ? t(($) => $.lifeos.submitting)
                  : t(($) =>
                      localAuthConfigured
                        ? $.lifeos.login
                        : $.lifeos.create_login,
                    )}
              </Button>
            </form>
          </CardContent>
        </Card>
      </main>
    </div>
  );
}
