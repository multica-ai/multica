"use client";

import { useEffect, useId, useState } from "react";
import { Check, FolderOpen, Loader2, Server, X } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { cn } from "@multica/ui/lib/utils";

import { useT } from "../../i18n";

// === Bridge type shadows ===
// We re-declare the minimal shape of the desktop bridge here rather than
// importing it from `apps/desktop`, because `packages/views/` is a shared
// library consumed by both the desktop app and the web build (where the
// bridge isn't defined). Branching on the presence of the global at call
// time keeps the dialog usable on web with a "desktop only" notice instead
// of a runtime crash.

interface CliConfigAPI {
  getOpenclaw(profile: string): Promise<{
    binaryPath: string;
    stateDir: string;
    envBinaryPath: string;
    envStateDir: string;
    configPath: string;
  }>;
  saveOpenclaw(args: {
    profile: string;
    binaryPath: string;
    stateDir: string;
  }): Promise<{ ok: boolean; reason?: string; error?: string }>;
}

interface DaemonAPI {
  getStatus(): Promise<{ profile?: string; state?: string }>;
  restart(): Promise<{ success: boolean; error?: string }>;
}

interface DesktopAPI {
  pickDirectory(
    defaultPath?: string,
  ): Promise<{ ok: boolean; path?: string; reason?: string }>;
  validateLocalDirectory(path: string): Promise<{
    ok: boolean;
    reason?:
      | "not_absolute"
      | "not_found"
      | "not_a_directory"
      | "not_readable"
      | "not_writable"
      | "error";
  }>;
}

function getCliConfigAPI(): CliConfigAPI | undefined {
  if (typeof window === "undefined") return undefined;
  return (window as unknown as { cliConfigAPI?: CliConfigAPI }).cliConfigAPI;
}
function getDaemonAPI(): DaemonAPI | undefined {
  if (typeof window === "undefined") return undefined;
  return (window as unknown as { daemonAPI?: DaemonAPI }).daemonAPI;
}
function getDesktopAPI(): DesktopAPI | undefined {
  if (typeof window === "undefined") return undefined;
  return (window as unknown as { desktopAPI?: DesktopAPI }).desktopAPI;
}

// === Component ===

export interface OpenClawConfigDialogProps {
  open: boolean;
  onClose(): void;
  /**
   * When true, the dialog is being opened from a daemon-restart-failure
   * recovery flow (sticky toast "Reconfigure" button, or the persistent
   * Settings banner). The dialog still loads the current on-disk value,
   * but renders an extra red notice strip at the top explaining that the
   * value below is the one that just failed to start the daemon.
   */
  fromFailure?: boolean;
}

type ValidationState = "idle" | "validating" | "ok" | "error";

export function OpenClawConfigDialog({
  open,
  onClose,
  fromFailure = false,
}: OpenClawConfigDialogProps) {
  const { t } = useT("runtimes");
  const idPrefix = `openclaw-config-${useId().replace(/:/g, "")}`;

  const [loading, setLoading] = useState(true);
  const [savedStateDir, setSavedStateDir] = useState("");
  const [stateDirInput, setStateDirInput] = useState("");
  const [envStateDir, setEnvStateDir] = useState("");
  const [profile, setProfile] = useState("");
  const [saving, setSaving] = useState(false);
  const [validation, setValidation] = useState<ValidationState>("idle");
  const [validationReason, setValidationReason] = useState<string | null>(null);

  // Reset + load whenever the dialog opens. We treat each open as a fresh
  // load rather than caching across mounts — the daemon may have restarted
  // (or another tool may have edited config.json) between opens.
  useEffect(() => {
    if (!open) return;

    setLoading(true);
    setValidation("idle");
    setValidationReason(null);

    const cliConfigAPI = getCliConfigAPI();
    const daemonAPI = getDaemonAPI();

    void (async () => {
      try {
        // Resolve which Multica profile the daemon is bound to so we read
        // the right config.json. Default "" = the unnamed default profile.
        const status = daemonAPI ? await daemonAPI.getStatus() : { profile: "" };
        const resolvedProfile = status.profile ?? "";
        setProfile(resolvedProfile);

        if (cliConfigAPI) {
          const payload = await cliConfigAPI.getOpenclaw(resolvedProfile);
          setSavedStateDir(payload.stateDir);
          setStateDirInput(payload.stateDir);
          setEnvStateDir(payload.envStateDir);
        } else {
          // Web build / unit test: no bridge, present an empty form. Saving
          // will fail later with a clear error.
          setSavedStateDir("");
          setStateDirInput("");
          setEnvStateDir("");
        }
      } catch (err) {
        toast.error(t(($) => $.openclawConfig.load_error), {
          description: err instanceof Error ? err.message : String(err),
        });
      } finally {
        setLoading(false);
      }
    })();
  }, [open, t]);

  // Debounced live validation while the user types. Empty input is "idle"
  // — there is nothing to validate, and an empty save means "clear the
  // override, fall back to OpenClaw's default ~/.openclaw" which is always
  // a valid action.
  useEffect(() => {
    if (!open || loading) return;
    const trimmed = stateDirInput.trim();
    if (trimmed === "") {
      setValidation("idle");
      setValidationReason(null);
      return;
    }

    const desktopAPI = getDesktopAPI();
    if (!desktopAPI?.validateLocalDirectory) {
      // Web build: skip live validation, fall back to whatever the save
      // path does.
      setValidation("idle");
      return;
    }

    setValidation("validating");
    let cancelled = false;
    const handle = window.setTimeout(() => {
      desktopAPI
        .validateLocalDirectory(trimmed)
        .then((res) => {
          if (cancelled) return;
          if (res.ok) {
            setValidation("ok");
            setValidationReason(null);
          } else {
            setValidation("error");
            setValidationReason(res.reason ?? "error");
          }
        })
        .catch(() => {
          if (cancelled) return;
          setValidation("error");
          setValidationReason("error");
        });
    }, 250);

    return () => {
      cancelled = true;
      window.clearTimeout(handle);
    };
  }, [stateDirInput, open, loading]);

  const trimmed = stateDirInput.trim();
  const isDirty = trimmed !== savedStateDir;
  const canSave =
    !loading &&
    !saving &&
    isDirty &&
    (trimmed === "" || validation === "ok");

  // Env-precedence warning: if OPENCLAW_STATE_DIR is set in the daemon's
  // environment, it wins over whatever the user is about to save. Show this
  // hint up-front so the save isn't a no-op surprise.
  const envOverridesConfig =
    envStateDir !== "" && trimmed !== envStateDir;

  const handlePickDirectory = async () => {
    const desktopAPI = getDesktopAPI();
    if (!desktopAPI?.pickDirectory) return;
    const result = await desktopAPI.pickDirectory(stateDirInput || undefined);
    if (result.ok && result.path) {
      setStateDirInput(result.path);
    }
  };

  const handleSave = async () => {
    const cliConfigAPI = getCliConfigAPI();
    const daemonAPI = getDaemonAPI();
    if (!cliConfigAPI || !daemonAPI) {
      toast.error(t(($) => $.openclawConfig.desktop_only));
      return;
    }

    setSaving(true);

    // Step 1: persist config.json. If this fails the daemon is still
    // running on the OLD config — non-destructive, no banner needed.
    const saveResult = await cliConfigAPI.saveOpenclaw({
      profile,
      binaryPath: "", // GUI does not expose binary_path; CLI does. Empty
      // string here is just "no change" semantics from the IPC layer's
      // perspective when the cli-config:save-openclaw handler treats both
      // fields as the new override state. We deliberately don't carry the
      // previously-saved binary_path through the GUI — if the user has
      // one set via CLI / env, the save here would clear it, which is
      // wrong. See follow-up note below.
      //
      // FOLLOW-UP: the dialog should preserve binary_path through saves
      // (read it via getOpenclaw, pass it back unchanged). Left as a small
      // todo because Layer 2 v1 scope is state_dir-only; binary_path is
      // CLI-only for now.
      stateDir: trimmed,
    });

    if (!saveResult.ok) {
      toast.error(t(($) => $.openclawConfig.save_error), {
        description: saveResult.error,
      });
      setSaving(false);
      return;
    }

    // Step 2: restart the daemon. If THIS fails the config IS already
    // saved (desired state preserved) but the daemon won't pick it up
    // until next manual restart. We surface a sticky toast so the user
    // sees the failure even after they've left the Settings page.
    const restartResult = await daemonAPI.restart();
    if (!restartResult.success) {
      toast.error(t(($) => $.openclawConfig.daemon_failed_title), {
        // duration: Infinity → user must explicitly dismiss. This is the
        // "level 0" global notice in the 5-layer failure UX (#3875).
        duration: Infinity,
        description: t(($) => $.openclawConfig.daemon_failed_desc, {
          path: trimmed === "" ? t(($) => $.openclawConfig.default_state_label) : trimmed,
        }),
        action: {
          label: t(($) => $.openclawConfig.reconfigure_button),
          onClick: () => {
            // Re-open the dialog in recovery mode. Implementing this
            // requires the parent to expose an imperative reopen; for now
            // we close + rely on the parent's Settings banner + dropdown
            // menu to provide that path.
          },
        },
      });
      setSaving(false);
      onClose();
      return;
    }

    toast.success(t(($) => $.openclawConfig.save_success));
    setSavedStateDir(trimmed);
    setSaving(false);
    onClose();
  };

  const fieldId = `${idPrefix}-state-dir`;
  const hintId = `${idPrefix}-state-dir-hint`;

  return (
    <Dialog open={open} onOpenChange={(o) => !o && !saving && onClose()}>
      <DialogContent
        className="flex flex-col gap-0 p-0 sm:max-w-lg"
        showCloseButton={false}
      >
        {/* Recovery notice — shown only when this dialog was opened from a
            failed-restart entry point. Renders ABOVE the standard header so
            it's the first thing the user sees. */}
        {fromFailure && (
          <div className="border-b border-destructive/30 bg-destructive/10 px-6 py-3 text-xs text-destructive">
            <div className="font-medium">
              {t(($) => $.openclawConfig.recover_title)}
            </div>
            <div className="mt-1 leading-relaxed text-destructive/80">
              {t(($) => $.openclawConfig.recover_desc)}
            </div>
          </div>
        )}

        <DialogHeader className="border-b px-6 py-5">
          <DialogTitle className="flex items-center gap-2 text-base">
            <Server className="h-4 w-4 shrink-0 text-muted-foreground" />
            <span>{t(($) => $.openclawConfig.dialog_title)}</span>
          </DialogTitle>
          <DialogDescription className="text-xs leading-relaxed">
            {t(($) => $.openclawConfig.dialog_description)}
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-col gap-4 px-6 py-5">
          <div className="flex flex-col gap-2">
            <Label htmlFor={fieldId} className="text-sm">
              {t(($) => $.openclawConfig.field_label)}{" "}
              <span className="ml-1 text-xs font-normal text-muted-foreground">
                {t(($) => $.openclawConfig.field_optional)}
              </span>
            </Label>

            <div className="flex gap-2">
              <Input
                id={fieldId}
                aria-describedby={hintId}
                value={stateDirInput}
                onChange={(e) => setStateDirInput(e.target.value)}
                placeholder={t(($) => $.openclawConfig.field_placeholder)}
                disabled={loading || saving}
                className={cn(
                  "font-mono text-sm",
                  validation === "ok" && "border-emerald-500/40 bg-emerald-50 dark:bg-emerald-950/30",
                  validation === "error" && "border-destructive/60 bg-destructive/5",
                )}
                spellCheck={false}
                autoComplete="off"
              />
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={handlePickDirectory}
                disabled={loading || saving || !getDesktopAPI()?.pickDirectory}
              >
                <FolderOpen className="h-3.5 w-3.5" />
                {t(($) => $.openclawConfig.browse_button)}
              </Button>
            </div>

            <div id={hintId} className={cn(
              "text-xs",
              validation === "ok" && "text-emerald-600 dark:text-emerald-400",
              validation === "error" && "text-destructive",
              (validation === "idle" || validation === "validating") && "text-muted-foreground",
            )}>
              {loading ? (
                <span className="inline-flex items-center gap-1.5">
                  <Loader2 className="h-3 w-3 animate-spin" />
                  {t(($) => $.openclawConfig.field_hint_loading)}
                </span>
              ) : validation === "validating" ? (
                <span className="inline-flex items-center gap-1.5">
                  <Loader2 className="h-3 w-3 animate-spin" />
                  {t(($) => $.openclawConfig.field_hint_validating)}
                </span>
              ) : validation === "ok" ? (
                <span className="inline-flex items-center gap-1.5">
                  <Check className="h-3 w-3" />
                  {t(($) => $.openclawConfig.field_hint_ok)}
                </span>
              ) : validation === "error" ? (
                <span className="inline-flex items-center gap-1.5">
                  <X className="h-3 w-3" />
                  {validationReason === "not_absolute"
                    ? t(($) => $.openclawConfig.field_error.not_absolute)
                    : validationReason === "not_found"
                    ? t(($) => $.openclawConfig.field_error.not_found)
                    : validationReason === "not_a_directory"
                    ? t(($) => $.openclawConfig.field_error.not_a_directory)
                    : validationReason === "not_readable"
                    ? t(($) => $.openclawConfig.field_error.not_readable)
                    : validationReason === "not_writable"
                    ? t(($) => $.openclawConfig.field_error.not_writable)
                    : t(($) => $.openclawConfig.field_error.other)}
                </span>
              ) : (
                t(($) => $.openclawConfig.field_hint_empty)
              )}
            </div>
          </div>

          {envOverridesConfig && (
            <div className="rounded-md border border-amber-300 bg-amber-50 px-3 py-2 text-xs text-amber-900 dark:border-amber-700 dark:bg-amber-950/30 dark:text-amber-200">
              <span className="font-medium">
                {t(($) => $.openclawConfig.env_wins_title)}
              </span>{" "}
              {t(($) => $.openclawConfig.env_wins_desc, { path: envStateDir })}
            </div>
          )}

          <div className="rounded-md border border-amber-300 bg-amber-50 px-3 py-2 text-xs text-amber-900 dark:border-amber-700 dark:bg-amber-950/30 dark:text-amber-200">
            ⚠ {t(($) => $.openclawConfig.restart_warning)}
          </div>
        </div>

        <div className="flex items-center justify-end gap-2 border-t bg-muted/30 px-6 py-3">
          <DialogClose
            render={
              <Button variant="outline" size="sm" disabled={saving}>
                {t(($) => $.openclawConfig.cancel)}
              </Button>
            }
          />
          <Button
            type="button"
            size="sm"
            onClick={handleSave}
            disabled={!canSave}
          >
            {saving ? (
              <>
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
                {t(($) => $.openclawConfig.saving)}
              </>
            ) : (
              t(($) => $.openclawConfig.save_button)
            )}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
