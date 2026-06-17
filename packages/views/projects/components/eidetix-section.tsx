"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight, Lock } from "lucide-react";
import { toast } from "sonner";
import {
  projectEidetixOptions,
  useSetProjectEidetix,
  useToggleProjectEidetix,
  useClearProjectEidetix,
} from "@multica/core/projects";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentMember } from "@multica/core/permissions";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { useT } from "../../i18n";

export function ProjectEidetixSection({ projectId }: { projectId: string }) {
  const { t } = useT("projects");
  const wsId = useWorkspaceId();
  const { role } = useCurrentMember(wsId);
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState(false);
  const [token, setToken] = useState("");
  const [label, setLabel] = useState("");

  const isAdmin = role === "owner" || role === "admin";
  const { data } = useQuery({
    ...projectEidetixOptions(wsId, projectId),
    enabled: isAdmin,
  });
  const setM = useSetProjectEidetix(projectId);
  const toggleM = useToggleProjectEidetix(projectId);
  const clearM = useClearProjectEidetix(projectId);

  if (!isAdmin) return null;

  const cfg = data ?? {
    configured: false,
    enabled: false,
    endpoint_url: "",
    graph_label: "",
  };

  function submitToken() {
    if (!token.trim()) return;
    setM.mutate(
      { token: token.trim(), graph_label: label.trim() || undefined },
      {
        onSuccess: () => {
          setToken("");
          setLabel("");
          setEditing(false);
          toast.success(t(($) => $.eidetix.saved));
        },
        onError: (err: unknown) => {
          const status = (err as { status?: number })?.status;
          toast.error(
            status === 503
              ? t(($) => $.eidetix.error_server_disabled)
              : t(($) => $.eidetix.error_generic),
          );
        },
      },
    );
  }

  return (
    <div>
      <button
        type="button"
        className="flex w-full items-center gap-1 rounded-md px-2 py-1 text-xs font-medium text-muted-foreground transition-colors hover:bg-accent/70 hover:text-foreground"
        onClick={() => setOpen(!open)}
      >
        {t(($) => $.eidetix.title)}
        <ChevronRight
          className={`!size-3 shrink-0 stroke-[2.5] transition-transform ${open ? "rotate-90" : ""}`}
        />
      </button>

      {open && (
        <div className="space-y-2 pl-2 pt-1">
          <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <Lock className="size-3 shrink-0" />
            {cfg.configured
              ? t(($) => $.eidetix.status_configured)
              : t(($) => $.eidetix.status_not_configured)}
            {cfg.configured && cfg.graph_label ? ` · ${cfg.graph_label}` : ""}
          </p>

          {cfg.configured && !editing && (
            <div className="flex flex-wrap gap-1.5">
              <Button
                size="sm"
                variant="outline"
                onClick={() => toggleM.mutate(!cfg.enabled)}
                disabled={toggleM.isPending}
              >
                {cfg.enabled
                  ? t(($) => $.eidetix.disable)
                  : t(($) => $.eidetix.enable)}
              </Button>
              <Button size="sm" variant="outline" onClick={() => setEditing(true)}>
                {t(($) => $.eidetix.replace_token)}
              </Button>
              <Button
                size="sm"
                variant="outline"
                onClick={() => {
                  if (
                    typeof window !== "undefined" &&
                    !window.confirm(t(($) => $.eidetix.clear_confirm))
                  )
                    return;
                  clearM.mutate(undefined, {
                    onSuccess: () =>
                      toast.success(t(($) => $.eidetix.cleared)),
                  });
                }}
                disabled={clearM.isPending}
              >
                {t(($) => $.eidetix.clear)}
              </Button>
            </div>
          )}

          {(!cfg.configured || editing) && (
            <div className="space-y-1.5">
              <div className="space-y-1">
                <Label htmlFor="eidetix-token" className="text-xs">
                  {t(($) => $.eidetix.token_label)}
                </Label>
                <Input
                  id="eidetix-token"
                  type="password"
                  value={token}
                  placeholder={t(($) => $.eidetix.token_placeholder)}
                  onChange={(e) => setToken(e.target.value)}
                />
              </div>
              <div className="space-y-1">
                <Label htmlFor="eidetix-label" className="text-xs">
                  {t(($) => $.eidetix.graph_label)}
                </Label>
                <Input
                  id="eidetix-label"
                  value={label}
                  placeholder={
                    cfg.graph_label ||
                    t(($) => $.eidetix.graph_label_placeholder)
                  }
                  onChange={(e) => setLabel(e.target.value)}
                />
              </div>
              <div className="flex gap-1.5">
                <Button
                  size="sm"
                  onClick={submitToken}
                  disabled={!token.trim() || setM.isPending}
                >
                  {cfg.configured
                    ? t(($) => $.eidetix.save)
                    : t(($) => $.eidetix.set_token)}
                </Button>
                {editing && (
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => {
                      setEditing(false);
                      setToken("");
                      setLabel("");
                    }}
                  >
                    {t(($) => $.eidetix.cancel)}
                  </Button>
                )}
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
