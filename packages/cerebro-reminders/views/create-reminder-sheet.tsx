"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Bot, User } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Label } from "@multica/ui/components/ui/label";
import {
  NativeSelect,
  NativeSelectOptGroup,
  NativeSelectOption,
} from "@multica/ui/components/ui/native-select";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { useAuthStore } from "@multica/core/auth";

import { useCreateReminder } from "../core/mutations";
import type { CreateReminderInput, RecipientType } from "../core/types";
import { useCerebroReminderStrings } from "../strings";

// A pre-bound anchor passed in when the sheet is opened from a message / issue /
// project. When omitted, the sheet creates a free reminder (anchor "none").
export interface PrefilledAnchor {
  type: "comment" | "issue" | "project";
  id: string;
  /** Shown read-only so the user knows what they are attaching to. */
  label: string;
}

interface CreateReminderSheetProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  anchor?: PrefilledAnchor;
  /** Optional suggested reminder text (e.g. from the source message). */
  defaultText?: string;
}

// datetime-local <input> works in local time with no timezone suffix; convert
// both directions without dragging in a date library.
function toLocalInputValue(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

function fromLocalInputValue(v: string): Date | null {
  const d = new Date(v);
  return Number.isNaN(d.getTime()) ? null : d;
}

function addHours(h: number): Date {
  const d = new Date();
  d.setHours(d.getHours() + h, d.getMinutes(), 0, 0);
  return d;
}

function tomorrowNineAm(): Date {
  const d = new Date();
  d.setDate(d.getDate() + 1);
  d.setHours(9, 0, 0, 0);
  return d;
}

// Encodes the recipient native-select value as "type:id" so members and agents
// can share one control without colliding ids.
function recipientValue(type: RecipientType, id: string): string {
  return `${type}:${id}`;
}

/**
 * The unified "create reminder" surface (FIR-394): pick a recipient (a person
 * or an agent), write the text, choose a time — anchored to a message / issue /
 * project, or free-standing. An agent recipient needs an anchor (it fires as a
 * wakeup, which needs an issue context), so agents are only offered when the
 * sheet carries a comment/issue anchor; the backend enforces the same rule.
 */
export function CreateReminderSheet({
  open,
  onOpenChange,
  anchor,
  defaultText,
}: CreateReminderSheetProps) {
  const wsId = useWorkspaceId();
  const currentUserId = useAuthStore((s) => s.user?.id ?? "");
  const create = useCreateReminder();
  const str = useCerebroReminderStrings();

  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));

  // Agents can only receive reminders that have an issue context — i.e. a
  // comment or issue anchor. Project / free reminders are member-only.
  const agentsAllowed = anchor?.type === "comment" || anchor?.type === "issue";
  const activeAgents = useMemo(() => agents.filter((a) => !a.archived_at), [agents]);

  const [recipient, setRecipient] = useState("");
  const [text, setText] = useState("");
  const [when, setWhen] = useState(() => toLocalInputValue(tomorrowNineAm()));

  // Re-seed each time the sheet opens so a stale draft never leaks across opens.
  useEffect(() => {
    if (!open) return;
    setRecipient(recipientValue("member", currentUserId));
    setText(defaultText ?? "");
    setWhen(toLocalInputValue(tomorrowNineAm()));
  }, [open, currentUserId, defaultText]);

  const plannedAt = fromLocalInputValue(when);
  const isFree = !anchor;
  // A free reminder must carry text; anchored reminders may inherit a label.
  const textOk = !isFree || text.trim().length > 0;
  const canSave =
    plannedAt !== null &&
    plannedAt.getTime() > Date.now() &&
    Boolean(recipient) &&
    textOk &&
    !create.isPending;

  const applyPreset = (d: Date) => setWhen(toLocalInputValue(d));

  const submit = () => {
    if (!plannedAt || plannedAt.getTime() <= Date.now()) return;
    const [type, id] = recipient.split(":");
    const input: CreateReminderInput = {
      remind_at: plannedAt.toISOString(),
      text: text.trim() || undefined,
      recipient_type: type as RecipientType,
      recipient_id: id,
    };
    if (anchor?.type === "comment") input.message_id = anchor.id;
    if (anchor?.type === "issue") input.issue_id = anchor.id;
    if (anchor?.type === "project") input.project_id = anchor.id;

    create.mutate(input, {
      onSuccess: () => {
        onOpenChange(false);
        toast.success(str.toast_created);
      },
      onError: () => toast.error(str.toast_create_failed),
    });
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{str.create_title}</DialogTitle>
        </DialogHeader>

        <div className="space-y-4">
          {/* Anchor — read-only when opened from a message/issue/project. */}
          {anchor && (
            <div className="rounded-lg border border-border bg-muted/40 px-3 py-2 text-sm">
              <span className="text-muted-foreground">{str.create_about_prefix} </span>
              <span className="font-medium text-foreground">{anchor.label}</span>
            </div>
          )}

          {/* Recipient — a person, or (when anchored) an agent. */}
          <div className="space-y-1.5">
            <Label htmlFor="reminder-recipient">{str.create_recipient_label}</Label>
            <NativeSelect
              id="reminder-recipient"
              className="w-full"
              value={recipient}
              onChange={(e) => setRecipient(e.target.value)}
            >
              <NativeSelectOptGroup label={str.create_recipient_people}>
                {members.map((m) => (
                  <NativeSelectOption
                    key={m.user_id}
                    value={recipientValue("member", m.user_id)}
                  >
                    {m.user_id === currentUserId
                      ? `${m.name} ${str.create_recipient_you_suffix}`
                      : m.name}
                  </NativeSelectOption>
                ))}
              </NativeSelectOptGroup>
              {agentsAllowed && activeAgents.length > 0 && (
                <NativeSelectOptGroup label={str.create_recipient_agents}>
                  {activeAgents.map((a) => (
                    <NativeSelectOption
                      key={a.id}
                      value={recipientValue("agent", a.id)}
                    >
                      {a.name}
                    </NativeSelectOption>
                  ))}
                </NativeSelectOptGroup>
              )}
            </NativeSelect>
            <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
              {recipient.startsWith("agent:") ? (
                <>
                  <Bot className="size-3.5" />
                  {str.create_recipient_agent_hint}
                </>
              ) : (
                <>
                  <User className="size-3.5" />
                  {str.create_recipient_member_hint}
                </>
              )}
            </p>
          </div>

          {/* Text. */}
          <div className="space-y-1.5">
            <Label htmlFor="reminder-text">
              {str.create_text_label}
              {isFree ? "" : str.create_text_optional_suffix}
            </Label>
            <Textarea
              id="reminder-text"
              value={text}
              onChange={(e) => setText(e.target.value)}
              placeholder={
                isFree ? str.create_text_placeholder_free : str.create_text_placeholder_anchored
              }
              rows={2}
            />
          </div>

          {/* When — quick presets + a custom date/time. */}
          <div className="space-y-2">
            <Label htmlFor="reminder-when">{str.create_time_label}</Label>
            <div className="flex flex-wrap gap-2">
              <Button type="button" variant="outline" size="sm" onClick={() => applyPreset(addHours(1))}>
                {str.create_preset_1h}
              </Button>
              <Button type="button" variant="outline" size="sm" onClick={() => applyPreset(addHours(3))}>
                {str.create_preset_3h}
              </Button>
              <Button type="button" variant="outline" size="sm" onClick={() => applyPreset(tomorrowNineAm())}>
                {str.create_preset_tomorrow_9}
              </Button>
            </div>
            <input
              id="reminder-when"
              type="datetime-local"
              value={when}
              min={toLocalInputValue(new Date())}
              onChange={(e) => setWhen(e.target.value)}
              className="h-9 w-full rounded-lg border border-input bg-transparent px-2.5 text-sm outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
            />
          </div>

          <div className="flex justify-end gap-2 pt-1">
            <Button variant="ghost" onClick={() => onOpenChange(false)}>
              {str.create_cancel}
            </Button>
            <Button onClick={submit} disabled={!canSave}>
              {str.create_submit}
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
