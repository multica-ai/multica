"use client";

import { useEffect, useState } from "react";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Button } from "@multica/ui/components/ui/button";
import { ApiError, api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { useNavigation } from "../navigation";

type RedeemState =
  | { kind: "idle" }
  | { kind: "redeeming" }
  | { kind: "done" }
  | { kind: "needs-auth" }
  | { kind: "error"; message: string; localBindUrl?: string };

/** HTTPS bind page cannot call http://localhost:8080 — browser blocks mixed content. */
function isBlockedLocalApiFromHttpsPage(): boolean {
  if (typeof window === "undefined") return false;
  const base = api.getBaseUrl().trim();
  if (!base) return false;
  try {
    const apiUrl = new URL(base);
    return window.location.protocol === "https:" && apiUrl.protocol === "http:";
  } catch {
    return false;
  }
}

function localBindUrl(token: string): string {
  return `http://localhost:3000/wecom/bind?token=${encodeURIComponent(token)}`;
}

function redeemErrorMessage(err: unknown, token: string): RedeemState {
  if (err instanceof ApiError) {
    switch (err.status) {
      case 401:
        return { kind: "needs-auth" };
      case 403:
        return {
          kind: "error",
          message: "当前账号不是该工作区成员，请用 linklogis 工作区的账号登录后再绑定。",
        };
      case 409:
        return {
          kind: "error",
          message: "该企业微信账号已绑定到其他 Multica 用户。",
        };
      case 410:
        return {
          kind: "error",
          message: "绑定链接已过期或已被使用，请在企业微信里给 Bot 再发一条消息获取新链接。",
        };
    }
  }
  return {
    kind: "error",
    message: "绑定失败，链接可能已过期或已被使用。",
    localBindUrl: localBindUrl(token),
  };
}

export function WecomBindPage({ token }: { token: string | null }) {
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);
  const navigation = useNavigation();
  const [state, setState] = useState<RedeemState>({ kind: "idle" });
  const [authWaitTimedOut, setAuthWaitTimedOut] = useState(false);

  useEffect(() => {
    if (!token) {
      setState({ kind: "error", message: "缺少绑定 token。" });
      return;
    }
    if (isBlockedLocalApiFromHttpsPage()) {
      const url = localBindUrl(token);
      setState({
        kind: "error",
        message:
          "当前链接是 HTTPS（ngrok），但 API 配置为 http://localhost:8080，浏览器会拦截请求。请在 PC 用 localhost 打开绑定链接。",
        localBindUrl: url,
      });
      return;
    }
  }, [token]);

  useEffect(() => {
    if (!isLoading) {
      setAuthWaitTimedOut(false);
      return;
    }
    const timer = window.setTimeout(() => setAuthWaitTimedOut(true), 8000);
    return () => window.clearTimeout(timer);
  }, [isLoading]);

  useEffect(() => {
    if (!token) return;
    if (state.kind === "error" && state.localBindUrl) return;
    if (isLoading && !authWaitTimedOut) return;
    if (!user) {
      if (state.kind !== "needs-auth") {
        setState({ kind: "needs-auth" });
      }
      return;
    }
    // Same guard as LarkBindPage: only start from idle/needs-auth. Including
    // "redeeming" in deps without this early return would run cleanup and
    // discard the successful redeem callback, leaving the page stuck.
    if (state.kind !== "idle" && state.kind !== "needs-auth") return;

    setState({ kind: "redeeming" });
    void (async () => {
      try {
        await api.redeemWecomBindingToken(token);
        setState({ kind: "done" });
      } catch (err) {
        setState(redeemErrorMessage(err, token));
      }
    })();
  }, [token, user, isLoading, authWaitTimedOut, state.kind]);

  return (
    <div className="mx-auto flex min-h-screen max-w-md flex-col items-center justify-center p-6">
      <Card className="w-full">
        <CardContent className="space-y-4">
          <h1 className="text-lg font-semibold">绑定企业微信身份</h1>
          {isLoading || state.kind === "idle" || state.kind === "redeeming" ? (
            <p className="text-sm text-muted-foreground">
              {isLoading ? "正在验证登录状态…" : "正在绑定…"}
            </p>
          ) : state.kind === "needs-auth" ? (
            <>
              <p className="text-sm text-muted-foreground">请先登录 Multica 账号以完成绑定。</p>
              <Button
                size="sm"
                onClick={() =>
                  navigation.push(
                    `/login?next=${encodeURIComponent(
                      `/wecom/bind?token=${encodeURIComponent(token ?? "")}`,
                    )}`,
                  )
                }
              >
                去登录
              </Button>
            </>
          ) : state.kind === "done" ? (
            <p className="text-sm text-muted-foreground">绑定成功，可以回到企业微信继续对话。</p>
          ) : state.kind === "error" ? (
            <div className="space-y-3">
              <p className="text-sm text-destructive">{state.message}</p>
              {state.localBindUrl ? (
                <Button
                  size="sm"
                  onClick={() => {
                    window.location.assign(state.localBindUrl!);
                  }}
                >
                  在 localhost 打开绑定链接
                </Button>
              ) : null}
            </div>
          ) : null}
        </CardContent>
      </Card>
    </div>
  );
}
