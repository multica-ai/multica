import { useCallback, useEffect, useState } from "react";
import { Check, ChevronDown, Loader2, Server } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { useT } from "@multica/views/i18n";
import type { DesktopServersState } from "../../../shared/runtime-config";
import { switchDesktopServer } from "../platform/server-switch";

/**
 * Login-page server picker. Switching after login is done from
 * Settings → Servers; the shell shows a read-only environment hint under
 * the workspace switcher (and window title) instead.
 */
export function DesktopServerSwitcher({ className }: { className?: string }) {
  const { t } = useT("settings");
  const [servers, setServers] = useState<DesktopServersState | null>(null);
  const [switchingId, setSwitchingId] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let mounted = true;
    void window.desktopAPI.listServers().then((result) => {
      if (!mounted) return;
      if (result.ok) setServers(result.servers);
      else setError(result.error);
    });
    return () => {
      mounted = false;
    };
  }, []);

  const active = servers?.servers.find((s) => s.id === servers.activeServerId);

  const handleSelect = useCallback(
    async (serverId: string) => {
      if (!servers || serverId === servers.activeServerId) return;
      setSwitchingId(serverId);
      setError(null);
      try {
        await switchDesktopServer(serverId);
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err));
        setSwitchingId(null);
      }
    },
    [servers],
  );

  if (!servers || servers.servers.length === 0) {
    return null;
  }

  const label = active?.name ?? t(($) => $.desktop.servers.title);
  const subtitle = active?.apiUrl;

  // Single non-editable server (typical dev): static label only.
  if (!servers.editable && servers.servers.length === 1) {
    return (
      <div className={className}>
        <div
          className="inline-flex items-center gap-1.5 rounded-md border bg-muted/30 px-2.5 py-1.5 text-xs text-muted-foreground"
          title={subtitle}
        >
          <Server className="size-3.5 shrink-0" />
          <span className="max-w-[200px] truncate">{label}</span>
        </div>
      </div>
    );
  }

  return (
    <div className={className}>
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <Button
              variant="outline"
              size="sm"
              className="max-w-full gap-1.5"
              disabled={switchingId !== null}
              aria-label={t(($) => $.desktop.servers.current_server_aria, {
                name: label,
              })}
              title={subtitle}
            >
              {switchingId ? (
                <Loader2 className="size-3.5 animate-spin" />
              ) : (
                <Server className="size-3.5 shrink-0" />
              )}
              <span className="max-w-[200px] truncate">{label}</span>
              <ChevronDown className="size-3 opacity-60" />
            </Button>
          }
        />
        <DropdownMenuContent align="center" className="min-w-[260px]">
          <DropdownMenuLabel>
            {t(($) => $.desktop.servers.switch_menu)}
          </DropdownMenuLabel>
          <DropdownMenuSeparator />
          {servers.servers.map((server) => {
            const isActive = server.id === servers.activeServerId;
            return (
              <DropdownMenuItem
                key={server.id}
                disabled={!servers.editable || switchingId !== null}
                onClick={() => void handleSelect(server.id)}
                className="flex flex-col items-start gap-0.5"
              >
                <span className="flex w-full items-center justify-between gap-2">
                  <span className="font-medium">{server.name}</span>
                  {isActive ? <Check className="size-3.5 text-success" /> : null}
                </span>
                <span className="max-w-[280px] truncate font-mono text-[10px] text-muted-foreground">
                  {server.apiUrl}
                </span>
              </DropdownMenuItem>
            );
          })}
        </DropdownMenuContent>
      </DropdownMenu>
      {error ? (
        <p className="mt-1.5 text-center text-xs text-destructive">{error}</p>
      ) : null}
    </div>
  );
}
