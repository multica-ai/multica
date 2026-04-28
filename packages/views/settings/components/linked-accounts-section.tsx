"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { Link2, Link2Off, Loader2 } from "lucide-react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import type { ExternalAccountBinding } from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@multica/ui/components/ui/card";
import { Skeleton } from "@multica/ui/components/ui/skeleton";

const accountLabels: Record<string, string> = {
  dingtalk: "DingTalk",
  google: "Google",
};

export function LinkedAccountsSection() {
  const [bindings, setBindings] = useState<ExternalAccountBinding[]>([]);
  const [loading, setLoading] = useState(true);
  const [removingBindingId, setRemovingBindingId] = useState<string | null>(null);
  const [startingBinding, setStartingBinding] = useState(false);

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
  const googleBinding = bindingByProvider.get("google");

  const handleDisconnect = async (binding: ExternalAccountBinding) => {
    setRemovingBindingId(binding.id);
    try {
      await api.deleteNotificationBinding(binding.id);
      toast.success(`${accountLabels[binding.provider] ?? binding.provider} disconnected`);
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
        redirect_uri: `${window.location.origin}/auth/callback`,
      });
      window.location.assign(auth_url);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to start DingTalk binding");
      setStartingBinding(false);
    }
  };

  const handleGoogleConnect = async () => {
    setStartingBinding(true);
    try {
      const { auth_url } = await api.startGoogleBinding({
        next_path: window.location.pathname,
        redirect_uri: `${window.location.origin}/auth/callback`,
      });
      window.location.assign(auth_url);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to start Google binding");
      setStartingBinding(false);
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

          {/* Google Card */}
          <Card>
            <CardHeader>
              <CardTitle className="text-base">Google</CardTitle>
              <CardDescription>
                {googleBinding
                  ? "Your Google account is linked."
                  : "Link your Google account to enable account association."}
              </CardDescription>
            </CardHeader>
            <CardContent className="flex items-center justify-between gap-4">
              <div className="space-y-1">
                <div className="flex items-center gap-2">
                  <span className="font-medium">
                    {googleBinding?.display_name ?? "No Google account connected"}
                  </span>
                  <Badge variant={googleBinding ? "secondary" : "outline"}>
                    {googleBinding?.status ?? "not connected"}
                  </Badge>
                </div>
                {googleBinding && (
                  <p className="text-sm text-muted-foreground">
                    ID: {googleBinding.external_user_id}
                  </p>
                )}
              </div>

              {googleBinding ? (
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => {
                    void handleDisconnect(googleBinding);
                  }}
                  disabled={removingBindingId !== null}
                >
                  {removingBindingId === googleBinding.id ? (
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
                    void handleGoogleConnect();
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
        </>
      )}
    </section>
  );
}
