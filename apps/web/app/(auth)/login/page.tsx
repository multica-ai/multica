"use client";

import { Suspense, useState, useEffect, useCallback } from "react";
import { useSearchParams, useRouter } from "next/navigation";
import { useAuthStore } from "@/features/auth";
import { useWorkspaceStore } from "@/features/workspace";
import { api } from "@/shared/api";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
  CardFooter,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import {
  InputOTP,
  InputOTPGroup,
  InputOTPSlot,
} from "@/components/ui/input-otp";
import type { User } from "@/shared/types";

interface AuthConfig {
  email: boolean;
  google: boolean;
  github: boolean;
}

function validateCliCallback(cliCallback: string): boolean {
  try {
    const cbUrl = new URL(cliCallback);
    if (cbUrl.protocol !== "http:") return false;
    if (cbUrl.hostname !== "localhost" && cbUrl.hostname !== "127.0.0.1")
      return false;
    return true;
  } catch {
    return false;
  }
}

function redirectToCliCallback(
  cliCallback: string,
  token: string,
  cliState: string
) {
  const separator = cliCallback.includes("?") ? "&" : "?";
  window.location.href = `${cliCallback}${separator}token=${encodeURIComponent(token)}&state=${encodeURIComponent(cliState)}`;
}

function LoginPageContent() {
  const router = useRouter();
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);
  const sendCode = useAuthStore((s) => s.sendCode);
  const verifyCode = useAuthStore((s) => s.verifyCode);
  const hydrateWorkspace = useWorkspaceStore((s) => s.hydrateWorkspace);
  const searchParams = useSearchParams();

  // Already authenticated — redirect to dashboard
  useEffect(() => {
    if (!isLoading && user && !searchParams.get("cli_callback")) {
      router.replace(searchParams.get("next") || "/issues");
    }
  }, [isLoading, user, router, searchParams]);

  const [authConfig, setAuthConfig] = useState<AuthConfig | null>(null);
  const [step, setStep] = useState<"email" | "code" | "cli_confirm">("email");
  const [email, setEmail] = useState("");
  const [code, setCode] = useState("");
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [cooldown, setCooldown] = useState(0);
  const [existingUser, setExistingUser] = useState<User | null>(null);

  const apiUrl = process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080";

  // Fetch auth config on mount.
  useEffect(() => {
    fetch(`${apiUrl}/auth/config`)
      .then((res) => res.json())
      .then((data: AuthConfig) => setAuthConfig(data))
      .catch(() => {
        // Fallback: show all methods if config endpoint is unreachable.
        setAuthConfig({ email: true, google: true, github: true });
      });
  }, [apiUrl]);

  const handleOAuth = (provider: string) => {
    const next = searchParams.get("next") || "";
    const cliCallback = searchParams.get("cli_callback") || "";
    const cliState = searchParams.get("cli_state") || "";
    const params = new URLSearchParams();
    if (next) params.set("next", next);
    if (cliCallback) params.set("cli_callback", cliCallback);
    if (cliState) params.set("cli_state", cliState);
    const qs = params.toString();
    window.location.href = `${apiUrl}/auth/${provider}${qs ? "?" + qs : ""}`;
  };

  // Check for existing session when CLI callback is present.
  useEffect(() => {
    const cliCallback = searchParams.get("cli_callback");
    if (!cliCallback) return;

    const token = localStorage.getItem("multica_token");
    if (!token) return;

    if (!validateCliCallback(cliCallback)) return;

    // Verify the existing token is still valid.
    api.setToken(token);
    api
      .getMe()
      .then((user) => {
        setExistingUser(user);
        setStep("cli_confirm");
      })
      .catch(() => {
        // Token expired/invalid — clear and fall through to normal login.
        api.setToken(null);
        localStorage.removeItem("multica_token");
      });
  }, [searchParams]);

  useEffect(() => {
    if (cooldown <= 0) return;
    const timer = setTimeout(() => setCooldown((c) => c - 1), 1000);
    return () => clearTimeout(timer);
  }, [cooldown]);

  // Show OAuth error if redirected back with error param.
  useEffect(() => {
    const oauthError = searchParams.get("error");
    if (oauthError === "oauth_denied") {
      setError("Authentication was cancelled or denied.");
    }
  }, [searchParams]);

  const handleCliAuthorize = async () => {
    const cliCallback = searchParams.get("cli_callback");
    const token = localStorage.getItem("multica_token");
    if (!cliCallback || !token) return;
    const cliState = searchParams.get("cli_state") || "";
    setSubmitting(true);
    redirectToCliCallback(cliCallback, token, cliState);
  };

  const handleSendCode = async (e?: React.FormEvent) => {
    e?.preventDefault();
    if (!email) {
      setError("Email is required");
      return;
    }
    setError("");
    setSubmitting(true);
    try {
      await sendCode(email);
      setStep("code");
      setCode("");
      setCooldown(10);
    } catch (err) {
      setError(
        err instanceof Error
          ? err.message
          : "Failed to send code. Make sure the server is running."
      );
    } finally {
      setSubmitting(false);
    }
  };

  const handleVerifyCode = useCallback(
    async (value: string) => {
      if (value.length !== 6) return;
      setError("");
      setSubmitting(true);
      try {
        const cliCallback = searchParams.get("cli_callback");
        if (cliCallback) {
          if (!validateCliCallback(cliCallback)) {
            setError("Invalid callback URL");
            setSubmitting(false);
            return;
          }
          const { token } = await api.verifyCode(email, value);
          const cliState = searchParams.get("cli_state") || "";
          redirectToCliCallback(cliCallback, token, cliState);
          return;
        }

        await verifyCode(email, value);
        const wsList = await api.listWorkspaces();
        const lastWsId = localStorage.getItem("multica_workspace_id");
        await hydrateWorkspace(wsList, lastWsId);
        router.push(searchParams.get("next") || "/issues");
      } catch (err) {
        setError(
          err instanceof Error ? err.message : "Invalid or expired code"
        );
        setCode("");
        setSubmitting(false);
      }
    },
    [email, verifyCode, hydrateWorkspace, router, searchParams]
  );

  const handleResend = async () => {
    if (cooldown > 0) return;
    setError("");
    try {
      await sendCode(email);
      setCooldown(10);
    } catch (err) {
      setError(
        err instanceof Error ? err.message : "Failed to resend code"
      );
    }
  };

  // CLI confirm step: user is already logged in, just authorize.
  if (step === "cli_confirm" && existingUser) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <Card className="w-full max-w-sm">
          <CardHeader className="text-center">
            <CardTitle className="text-2xl">Authorize CLI</CardTitle>
            <CardDescription>
              Allow the CLI to access Multica as{" "}
              <span className="font-medium text-foreground">
                {existingUser.email}
              </span>
              ?
            </CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            <Button
              onClick={handleCliAuthorize}
              disabled={submitting}
              className="w-full"
              size="lg"
            >
              {submitting ? "Authorizing..." : "Authorize"}
            </Button>
            <Button
              variant="ghost"
              className="w-full"
              onClick={() => {
                setExistingUser(null);
                setStep("email");
              }}
            >
              Use a different account
            </Button>
          </CardContent>
        </Card>
      </div>
    );
  }

  if (step === "code") {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <Card className="w-full max-w-sm">
          <CardHeader className="text-center">
            <CardTitle className="text-2xl">Check your email</CardTitle>
            <CardDescription>
              We sent a verification code to{" "}
              <span className="font-medium text-foreground">{email}</span>
            </CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col items-center gap-4">
            <InputOTP
              maxLength={6}
              value={code}
              onChange={(value) => {
                setCode(value);
                if (value.length === 6) handleVerifyCode(value);
              }}
              disabled={submitting}
            >
              <InputOTPGroup>
                <InputOTPSlot index={0} />
                <InputOTPSlot index={1} />
                <InputOTPSlot index={2} />
                <InputOTPSlot index={3} />
                <InputOTPSlot index={4} />
                <InputOTPSlot index={5} />
              </InputOTPGroup>
            </InputOTP>
            {error && (
              <p className="text-sm text-destructive">{error}</p>
            )}
            <div className="flex items-center gap-2 text-sm text-muted-foreground">
              <button
                type="button"
                onClick={handleResend}
                disabled={cooldown > 0}
                className="text-primary underline-offset-4 hover:underline disabled:text-muted-foreground disabled:no-underline disabled:cursor-not-allowed"
              >
                {cooldown > 0 ? `Resend in ${cooldown}s` : "Resend code"}
              </button>
            </div>
          </CardContent>
          <CardFooter>
            <Button
              variant="ghost"
              className="w-full"
              onClick={() => {
                setStep("email");
                setCode("");
                setError("");
              }}
            >
              Back
            </Button>
          </CardFooter>
        </Card>
      </div>
    );
  }

  // Derive which sections to show.
  const showEmail = authConfig?.email ?? true;
  const showGoogle = authConfig?.google ?? false;
  const showGithub = authConfig?.github ?? false;
  const hasOAuth = showGoogle || showGithub;

  return (
    <div className="flex min-h-screen items-center justify-center">
      <Card className="w-full max-w-sm">
        <CardHeader className="text-center">
          <CardTitle className="text-2xl">Multica</CardTitle>
          <CardDescription>Turn coding agents into real teammates</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          {hasOAuth && (
            <div className={`grid gap-2 ${showGoogle && showGithub ? "grid-cols-2" : "grid-cols-1"}`}>
              {showGoogle && (
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => handleOAuth("google")}
                  disabled={submitting}
                >
                  <svg className="mr-2 h-4 w-4" viewBox="0 0 24 24">
                    <path
                      d="M22.56 12.25c0-.78-.07-1.53-.2-2.25H12v4.26h5.92a5.06 5.06 0 0 1-2.2 3.32v2.77h3.57c2.08-1.92 3.28-4.74 3.28-8.1z"
                      fill="#4285F4"
                    />
                    <path
                      d="M12 23c2.97 0 5.46-.98 7.28-2.66l-3.57-2.77c-.98.66-2.23 1.06-3.71 1.06-2.86 0-5.29-1.93-6.16-4.53H2.18v2.84C3.99 20.53 7.7 23 12 23z"
                      fill="#34A853"
                    />
                    <path
                      d="M5.84 14.09c-.22-.66-.35-1.36-.35-2.09s.13-1.43.35-2.09V7.07H2.18C1.43 8.55 1 10.22 1 12s.43 3.45 1.18 4.93l2.85-2.22.81-.62z"
                      fill="#FBBC05"
                    />
                    <path
                      d="M12 5.38c1.62 0 3.06.56 4.21 1.64l3.15-3.15C17.45 2.09 14.97 1 12 1 7.7 1 3.99 3.47 2.18 7.07l3.66 2.84c.87-2.6 3.3-4.53 6.16-4.53z"
                      fill="#EA4335"
                    />
                  </svg>
                  Google
                </Button>
              )}
              {showGithub && (
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => handleOAuth("github")}
                  disabled={submitting}
                >
                  <svg className="mr-2 h-4 w-4" viewBox="0 0 24 24" fill="currentColor">
                    <path d="M12 0c-6.626 0-12 5.373-12 12 0 5.302 3.438 9.8 8.207 11.387.599.111.793-.261.793-.577v-2.234c-3.338.726-4.033-1.416-4.033-1.416-.546-1.387-1.333-1.756-1.333-1.756-1.089-.745.083-.729.083-.729 1.205.084 1.839 1.237 1.839 1.237 1.07 1.834 2.807 1.304 3.492.997.107-.775.418-1.305.762-1.604-2.665-.305-5.467-1.334-5.467-5.931 0-1.311.469-2.381 1.236-3.221-.124-.303-.535-1.524.117-3.176 0 0 1.008-.322 3.301 1.23.957-.266 1.983-.399 3.003-.404 1.02.005 2.047.138 3.006.404 2.291-1.552 3.297-1.23 3.297-1.23.653 1.653.242 2.874.118 3.176.77.84 1.235 1.911 1.235 3.221 0 4.609-2.807 5.624-5.479 5.921.43.372.823 1.102.823 2.222v3.293c0 .319.192.694.801.576 4.765-1.589 8.199-6.086 8.199-11.386 0-6.627-5.373-12-12-12z" />
                  </svg>
                  GitHub
                </Button>
              )}
            </div>
          )}
          {hasOAuth && showEmail && (
            <div className="relative">
              <div className="absolute inset-0 flex items-center">
                <span className="w-full border-t" />
              </div>
              <div className="relative flex justify-center text-xs uppercase">
                <span className="bg-card px-2 text-muted-foreground">
                  Or continue with email
                </span>
              </div>
            </div>
          )}
          {showEmail && (
            <form id="login-form" onSubmit={handleSendCode} className="space-y-4">
              <div className="space-y-2">
                <Label htmlFor="email">Email</Label>
                <Input
                  id="email"
                  type="email"
                  placeholder="you@example.com"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  required
                />
              </div>
            </form>
          )}
          {error && (
            <p className="text-sm text-destructive">{error}</p>
          )}
        </CardContent>
        {showEmail && (
          <CardFooter>
            <Button
              type="submit"
              form="login-form"
              disabled={submitting}
              className="w-full"
              size="lg"
            >
              {submitting ? "Sending code..." : "Continue"}
            </Button>
          </CardFooter>
        )}
      </Card>
    </div>
  );
}

export default function LoginPage() {
  return (
    <Suspense fallback={null}>
      <LoginPageContent />
    </Suspense>
  );
}
