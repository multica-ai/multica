"use client";

// CEREBRO-PATCH(enter-preference-section): cerebro modification of upstream file

import { useState } from "react";
import { Keyboard, Link as LinkIcon } from "lucide-react";
import { Switch } from "@multica/ui/components/ui/switch";
import { Label } from "@multica/ui/components/ui/label";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { toast } from "sonner";
import { useAuthStore } from "@multica/core/auth";
import { api } from "@multica/core/api";
import {
  ISSUE_LINK_OPEN_MODE_KEY,
  SUBMIT_ON_ENTER_KEY,
  type IssueLinkOpenMode,
} from "./use-submit-on-enter";

/**
 * Fork-specific: lets the user choose between Enter-to-send and
 * Cmd/Ctrl+Enter-to-send across all message composers (chat, comments, replies).
 *
 * Default is Cmd/Ctrl+Enter (preserves the long-form composer behaviour);
 * users opt into Enter-to-send when they treat composers as chat inputs.
 */
export function EnterPreferenceSection() {
  const user = useAuthStore((s) => s.user);
  const setUser = useAuthStore((s) => s.setUser);
  const [saving, setSaving] = useState(false);
  const [linkSaving, setLinkSaving] = useState(false);

  const submitOnEnter = user?.preferences?.[SUBMIT_ON_ENTER_KEY] === true;
  const issueLinkOpenMode: IssueLinkOpenMode =
    user?.preferences?.[ISSUE_LINK_OPEN_MODE_KEY] === "always_new_tab"
      ? "always_new_tab"
      : "modifier_click";
  const isMac = typeof navigator !== "undefined" && /Mac|iPhone|iPad|iPod/i.test(navigator.platform);
  const modKey = isMac ? "⌘" : "Ctrl";

  const handleChange = async (next: boolean) => {
    setSaving(true);
    try {
      const updated = await api.updateMyPreferences({ [SUBMIT_ON_ENTER_KEY]: next });
      setUser(updated);
      toast.success(next ? "Enter now sends" : `${modKey}+Enter now sends`);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to update preference");
    } finally {
      setSaving(false);
    }
  };

  const handleIssueLinkModeChange = async (next: IssueLinkOpenMode) => {
    if (next === issueLinkOpenMode) return;
    setLinkSaving(true);
    try {
      const updated = await api.updateMyPreferences({ [ISSUE_LINK_OPEN_MODE_KEY]: next });
      setUser(updated);
      toast.success(
        next === "always_new_tab"
          ? "Issue links now open in a new tab"
          : `Issue links now use ${modKey}+click for new tabs`,
      );
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to update preference");
    } finally {
      setLinkSaving(false);
    }
  };

  return (
    <div className="space-y-8">
      <section className="space-y-4">
        <div className="flex items-center gap-2">
          <Keyboard className="h-4 w-4 text-muted-foreground" />
          <h2 className="text-sm font-semibold">Composer</h2>
        </div>

        <Card>
          <CardContent>
            <div className="flex items-start justify-between gap-4">
              <div className="space-y-1">
                <Label className="text-sm font-medium">Send messages with Enter</Label>
                <p className="text-xs text-muted-foreground">
                  {submitOnEnter
                    ? `Enter sends. Use Shift+Enter (or ${modKey}+Enter) for a new line.`
                    : `${modKey}+Enter sends. Enter inserts a new line.`}
                  <br />
                  Applies to chat, comments, and replies.
                </p>
              </div>
              <Switch
                checked={submitOnEnter}
                disabled={saving || !user}
                onCheckedChange={handleChange}
                aria-label="Send messages with Enter"
              />
            </div>
          </CardContent>
        </Card>
      </section>

      <section className="space-y-4">
        <div className="flex items-center gap-2">
          <LinkIcon className="h-4 w-4 text-muted-foreground" />
          <h2 className="text-sm font-semibold">Issue links</h2>
        </div>

        <Card>
          <CardContent>
            <div className="space-y-3" role="radiogroup" aria-label="Issue link behavior">
              <IssueLinkModeOption
                active={issueLinkOpenMode === "modifier_click"}
                disabled={linkSaving || !user}
                label="Use modifier-click"
                description={`Regular click opens in the current view. ${modKey}+click opens a new tab.`}
                onClick={() => handleIssueLinkModeChange("modifier_click")}
              />
              <IssueLinkModeOption
                active={issueLinkOpenMode === "always_new_tab"}
                disabled={linkSaving || !user}
                label="Always open a new tab"
                description="Regular click keeps the current issue or chat visible by opening the link separately."
                onClick={() => handleIssueLinkModeChange("always_new_tab")}
              />
            </div>
          </CardContent>
        </Card>
      </section>
    </div>
  );
}

function IssueLinkModeOption({
  active,
  disabled,
  label,
  description,
  onClick,
}: {
  active: boolean;
  disabled: boolean;
  label: string;
  description: string;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      role="radio"
      aria-checked={active}
      disabled={disabled}
      onClick={onClick}
      className={`flex w-full items-start gap-3 rounded-md border p-3 text-left transition-colors ${
        active
          ? "border-brand bg-brand/10"
          : "border-border hover:border-foreground/30 disabled:hover:border-border"
      } ${disabled ? "cursor-not-allowed opacity-60" : ""}`}
    >
      <span
        className={`mt-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded-full border ${
          active ? "border-brand" : "border-muted-foreground/40"
        }`}
        aria-hidden="true"
      >
        {active && <span className="h-2 w-2 rounded-full bg-brand" />}
      </span>
      <span className="space-y-1">
        <span className="block text-sm font-medium">{label}</span>
        <span className="block text-xs text-muted-foreground">{description}</span>
      </span>
    </button>
  );
}
