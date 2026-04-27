"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { Link2, Link2Off, Loader2, Mail } from "lucide-react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import type { ExternalAccountBinding, NotificationChannel } from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@multica/ui/components/ui/card";
import { Input } from "@multica/ui/components/ui/input";
import { Skeleton } from "@multica/ui/components/ui/skeleton";

const channelLabels: Partial<Record<NotificationChannel, string>> = {
  dingtalk: "DingTalk",
  email: "Email",
};

export function LinkedAccountsSection() {
  const [bindings, setBindings] = useState<ExternalAccountBinding[]>([]);
  const [loading, setLoading] = useState(true);
  const [removingBindingId, setRemovingBindingId] = useState<string | null>(null);
  const [startingBinding, setStartingBinding] = useState(false);

  // Email binding state
  const [emailInput, setEmailInput] = useState("");
  const [emailCodeInput, setEmailCodeInput] = useState("");
  const [emailStep, setEmailStep] = useState<"input" | "verify">("input");
  const [emailPending, setEmailPending] = useState(false);
  const [pendingEmail, setPendingEmail] = useState("");

  const loadBindings = useCallback(async () => {
    setLoading(true);
    try {
      const response = await api.listNotificationBindings();
      setBindings(response.bindings);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to load linked accounts");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void loadBindings();
  }, [loadBindings]);

  const bindingByProvider = useMemo(() => {
    const next = new Map<string, ExternalAccountBinding>();
    for (const binding of bindings) next.set(binding.provider, binding);
    return next;
  }, [bindings]);

  const dingTalkBinding = bindingByProvider.get("dingtalk");
  const emailBinding = bindingByProvider.get("email");

  const handleDisconnect = async (binding: ExternalAccountBinding) => {
    setRemovingBindingId(binding.id);
    try {
      await api.deleteNotificationBinding(binding.id);
      toast.success(`${channelLabels[binding.provider as NotificationChannel] ?? binding.provider} disconnected`);
      await loadBindings();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to disconnect account");
    } finally {
      setRemovingBindingId(null);
    }
  };

  const handleConnect = async () => {
    setStartingBinding(true);
    try {
      const { auth_url } = await api.startDingTalkBinding({
        next_path: window.location.pathname,
      });
      window.location.assign(auth_url);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to start DingTalk binding");
      setStartingBinding(false);
    }
  };

  const handleSendEmailCode = async () => {
    const email = emailInput.trim();
    if (!email) return;
    setEmailPending(true);
    try {
      await api.startEmailBinding({ email });
      setPendingEmail(email);
      setEmailStep("verify");
      toast.success("Verification code sent to " + email);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to send verification code");
    } finally {
      setEmailPending(false);
    }
  };

  const handleVerifyEmail = async () => {
    const code = emailCodeInput.trim();
    if (!code || !pendingEmail) return;
    setEmailPending(true);
    try {
      await api.verifyEmailBinding({ email: pendingEmail, code });
      toast.success("Email linked successfully");
      setEmailStep("input");
      setEmailInput("");
      setEmailCodeInput("");
      setPendingEmail("");
      await loadBindings();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Invalid or expired code");
    } finally {
      setEmailPending(false);
    }
  };

  return (
    <section className="space-y-4">
      <div className="space-y-1">
        <h2 className="text-sm font-semibold">Linked Accounts</h2>
        <p className="text-sm text-muted-foreground">
          Manage external accounts used by notification channels.
        </p>
      </div>

      {loading ? (
        <Skeleton className="h-32 w-full" />
      ) : (
        <>
          {/* DingTalk Card */}
          <Card>
            <CardHeader>
              <CardTitle className="text-base">DingTalk</CardTitle>
              <CardDescription>
                Link your DingTalk account here, then enable the DingTalk channel from Notifications.
              </CardDescription>
            </CardHeader>
            <CardContent className="flex items-center justify-between gap-4">
              <div className="space-y-1">
                <div className="flex items-center gap-2">
                  <span className="font-medium">
                    {dingTalkBinding?.display_name ?? "No DingTalk account connected"}
                  </span>
                  <Badge variant={dingTalkBinding ? "secondary" : "outline"}>
                    {dingTalkBinding?.status ?? "not connected"}
                  </Badge>
                </div>
                <p className="text-sm text-muted-foreground">
                  {dingTalkBinding
                    ? `External user: ${dingTalkBinding.external_user_id}`
                    : "Connect a DingTalk account to unlock external mention delivery."}
                </p>
              </div>

              {dingTalkBinding ? (
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => {
                    void handleDisconnect(dingTalkBinding);
                  }}
                  disabled={removingBindingId !== null}
                >
                  {removingBindingId === dingTalkBinding.id ? (
                    <Loader2 className="h-4 w-4 animate-spin" />
                  ) : (
                    <Link2Off className="h-4 w-4" />
                  )}
                  Disconnect
                </Button>
              ) : (
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => {
                    void handleConnect();
                  }}
                  disabled={removingBindingId !== null || startingBinding}
                >
                  {startingBinding ? (
                    <Loader2 className="h-4 w-4 animate-spin" />
                  ) : (
                    <Link2 className="h-4 w-4" />
                  )}
                  Connect
                </Button>
              )}
            </CardContent>
          </Card>

          {/* Email Card */}
          <Card>
            <CardHeader>
              <CardTitle className="text-base">Email</CardTitle>
              <CardDescription>
                Link an email address to receive notification emails when you are mentioned.
              </CardDescription>
            </CardHeader>
            <CardContent>
              {emailBinding ? (
                <div className="flex items-center justify-between gap-4">
                  <div className="space-y-1">
                    <div className="flex items-center gap-2">
                      <Mail className="h-4 w-4 text-muted-foreground" />
                      <span className="font-medium">{emailBinding.external_user_id}</span>
                      <Badge variant="secondary">{emailBinding.status}</Badge>
                    </div>
                  </div>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => {
                      void handleDisconnect(emailBinding);
                    }}
                    disabled={removingBindingId !== null}
                  >
                    {removingBindingId === emailBinding.id ? (
                      <Loader2 className="h-4 w-4 animate-spin" />
                    ) : (
                      <Link2Off className="h-4 w-4" />
                    )}
                    Disconnect
                  </Button>
                </div>
              ) : emailStep === "input" ? (
                <div className="flex items-center gap-2">
                  <Input
                    type="email"
                    placeholder="you@example.com"
                    value={emailInput}
                    onChange={(e) => setEmailInput(e.target.value)}
                    onKeyDown={(e) => {
                      if (e.key === "Enter") void handleSendEmailCode();
                    }}
                    disabled={emailPending}
                    className="max-w-xs"
                  />
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => {
                      void handleSendEmailCode();
                    }}
                    disabled={emailPending || !emailInput.trim()}
                  >
                    {emailPending ? (
                      <Loader2 className="h-4 w-4 animate-spin" />
                    ) : (
                      <Mail className="h-4 w-4" />
                    )}
                    Send Code
                  </Button>
                </div>
              ) : (
                <div className="space-y-2">
                  <p className="text-sm text-muted-foreground">
                    A verification code was sent to <strong>{pendingEmail}</strong>.
                  </p>
                  <div className="flex items-center gap-2">
                    <Input
                      type="text"
                      placeholder="Enter 6-digit code"
                      value={emailCodeInput}
                      onChange={(e) => setEmailCodeInput(e.target.value)}
                      onKeyDown={(e) => {
                        if (e.key === "Enter") void handleVerifyEmail();
                      }}
                      disabled={emailPending}
                      className="max-w-[160px]"
                      maxLength={6}
                    />
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => {
                        void handleVerifyEmail();
                      }}
                      disabled={emailPending || !emailCodeInput.trim()}
                    >
                      {emailPending ? (
                        <Loader2 className="h-4 w-4 animate-spin" />
                      ) : (
                        <Link2 className="h-4 w-4" />
                      )}
                      Verify
                    </Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => {
                        setEmailStep("input");
                        setEmailCodeInput("");
                        setPendingEmail("");
                      }}
                      disabled={emailPending}
                    >
                      Cancel
                    </Button>
                  </div>
                </div>
              )}
            </CardContent>
          </Card>
        </>
      )}
    </section>
  );
}
