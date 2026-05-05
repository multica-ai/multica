"use client";

// CEREBRO-PATCH(add-runtime-dialog): cerebro modification of upstream file

import { useEffect, useMemo, useState } from "react";
import { Plus, Copy, Check, Loader2, ExternalLink, RefreshCw } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
  DialogTrigger,
} from "@multica/ui/components/ui/dialog";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { api } from "@multica/core/api";
import type { CreateRuntimeSetupTokenResponse } from "@multica/core/types";
import { useNavigation } from "../../navigation";
import { toast } from "sonner";

const TICK_MS = 1000;

export function AddRuntimeDialog() {
  const [open, setOpen] = useState(false);
  const [deviceLabel, setDeviceLabel] = useState("");
  const [generating, setGenerating] = useState(false);
  const [token, setToken] = useState<CreateRuntimeSetupTokenResponse | null>(null);
  const [now, setNow] = useState(() => Date.now());
  const [copied, setCopied] = useState(false);
  const { push } = useNavigation();

  useEffect(() => {
    if (!token) return;
    const t = setInterval(() => setNow(Date.now()), TICK_MS);
    return () => clearInterval(t);
  }, [token]);

  const remainingMs = useMemo(() => {
    if (!token) return 0;
    const expires = new Date(token.expires_at).getTime();
    return Math.max(0, expires - now);
  }, [token, now]);
  const expired = token != null && remainingMs <= 0;

  const reset = () => {
    setToken(null);
    setCopied(false);
    setDeviceLabel("");
  };

  const generate = async () => {
    setGenerating(true);
    try {
      const resp = await api.createRuntimeSetupToken({
        device_label: deviceLabel.trim() || undefined,
      });
      setToken(resp);
      setNow(Date.now());
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to generate setup token");
    } finally {
      setGenerating(false);
    }
  };

  const copy = async () => {
    if (!token) return;
    try {
      await navigator.clipboard.writeText(token.install_command);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      toast.error("Failed to copy to clipboard");
    }
  };

  const goToDocs = () => {
    setOpen(false);
    push(`/${token?.workspace_slug ?? ""}/docs/runtimes`);
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(v) => {
        setOpen(v);
        if (!v) reset();
      }}
    >
      <DialogTrigger
        render={
          <Button size="sm" variant="outline">
            <Plus className="h-3.5 w-3.5" />
            Add runtime
          </Button>
        }
      />
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Add a new runtime</DialogTitle>
          <DialogDescription>
            Generate a one-line installer that sets up the multica daemon on
            another machine. The new runtime will appear in the list once it
            registers.
          </DialogDescription>
        </DialogHeader>

        {!token ? (
          <div className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="device-label">Device label (optional)</Label>
              <Input
                id="device-label"
                placeholder="e.g. Anders' MacBook"
                value={deviceLabel}
                onChange={(e) => setDeviceLabel(e.target.value)}
                disabled={generating}
              />
              <p className="text-xs text-muted-foreground">
                Used as the PAT name so you can recognise it later.
              </p>
            </div>
          </div>
        ) : (
          <div className="space-y-4">
            <div>
              <Label className="text-xs uppercase text-muted-foreground">
                Run this on the new machine
              </Label>
              <div className="mt-2 flex items-start gap-2 rounded-md border bg-muted/40 p-3">
                <code className="flex-1 break-all text-xs font-mono leading-relaxed">
                  {token.install_command}
                </code>
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={copy}
                  className="shrink-0"
                  aria-label="Copy install command"
                >
                  {copied ? (
                    <Check className="h-3.5 w-3.5 text-success" />
                  ) : (
                    <Copy className="h-3.5 w-3.5" />
                  )}
                </Button>
              </div>
            </div>

            <div className="flex items-center justify-between text-xs text-muted-foreground">
              <span>
                Single-use. Expires in{" "}
                <span className={expired ? "text-destructive" : "text-foreground"}>
                  {formatRemaining(remainingMs)}
                </span>
                .
              </span>
              <button
                onClick={() => {
                  reset();
                  generate();
                }}
                className="inline-flex items-center gap-1 hover:text-foreground"
              >
                <RefreshCw className="h-3 w-3" />
                Regenerate
              </button>
            </div>

            <div className="rounded-md border border-dashed p-3 text-xs text-muted-foreground">
              <strong className="block text-foreground">What this does</strong>
              Installs the multica CLI, exchanges this token for a per-user
              daemon token, writes <code>~/.multica/config.json</code>, and
              starts the daemon as a launchd job (macOS) or in the foreground
              (Linux). The setup token is consumed on first use.
            </div>
          </div>
        )}

        <DialogFooter className="flex flex-row items-center justify-between sm:justify-between">
          <button
            onClick={goToDocs}
            className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
          >
            <ExternalLink className="h-3 w-3" />
            Read full setup docs
          </button>
          {!token ? (
            <Button onClick={generate} disabled={generating}>
              {generating ? (
                <>
                  <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  Generating…
                </>
              ) : (
                "Generate install command"
              )}
            </Button>
          ) : (
            <Button variant="outline" onClick={() => setOpen(false)}>
              Close
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function formatRemaining(ms: number): string {
  if (ms <= 0) return "expired";
  const totalSeconds = Math.floor(ms / 1000);
  const m = Math.floor(totalSeconds / 60);
  const s = totalSeconds % 60;
  return `${m}m ${s.toString().padStart(2, "0")}s`;
}
