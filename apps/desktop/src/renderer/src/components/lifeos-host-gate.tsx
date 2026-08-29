import { useEffect, useState } from "react";
import { LoaderCircle } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { MulticaIcon } from "@multica/ui/components/common/multica-icon";
import { DragStrip } from "@multica/views/platform";
import type { LifeOSHostStatus } from "../../../shared/lifeos-host";

export function LifeOSHostGate({ children }: { children: React.ReactNode }) {
  const host = window.desktopAPI.lifeOSHost;
  const enabled = window.desktopAPI.appInfo.flavor === "lifeos";
  const [status, setStatus] = useState<LifeOSHostStatus>(
    host?.initialStatus ?? {
      state: enabled ? "offline" : "disabled",
      checkedAt: Date.now(),
    },
  );

  useEffect(() => {
    if (!enabled || !host) return undefined;
    return host.onStatusChange(setStatus);
  }, [enabled, host]);

  if (!enabled || status.state === "ready") return <>{children}</>;

  const starting = status.state === "starting";
  const title = starting ? "正在启动 LifeOS" : "LifeOS 需要恢复";
  const description = starting
    ? "正在恢复本地看板、数据库和 AI 执行器。完成后会自动进入工作台。"
    : status.message ??
      "本机服务当前不可用。恢复只会运行固定的 LifeOS 启动流程，不会执行其他命令。";

  return (
    <div className="flex h-screen flex-col bg-app-shell text-foreground">
      <DragStrip />
      <main className="flex min-h-0 flex-1 items-center justify-center px-6 pb-12">
        <section
          aria-live="polite"
          className="w-full max-w-md rounded-2xl border bg-card p-7 shadow-sm"
        >
          <div className="mb-5 flex items-center gap-3">
            <MulticaIcon bordered size="lg" />
            <div className="min-w-0">
              <p className="text-xs font-medium text-muted-foreground">
                AI 星耀 · 本机控制面板
              </p>
              <h1 className="text-xl font-semibold tracking-tight text-balance">
                {title}
              </h1>
            </div>
          </div>
          <p className="text-sm leading-6 text-muted-foreground text-pretty">
            {description}
          </p>
          <div className="mt-6 flex flex-wrap gap-2">
            <Button
              type="button"
              disabled={starting || !host}
              onClick={() => {
                void host?.ensure();
              }}
            >
              {starting ? (
                <>
                  <LoaderCircle
                    aria-hidden="true"
                    className="motion-safe:animate-spin"
                  />
                  正在恢复…
                </>
              ) : (
                "恢复本机服务"
              )}
            </Button>
            <Button
              type="button"
              variant="outline"
              disabled={!host}
              onClick={() => {
                void host?.openLogs();
              }}
            >
              打开运行日志
            </Button>
          </div>
        </section>
      </main>
    </div>
  );
}
