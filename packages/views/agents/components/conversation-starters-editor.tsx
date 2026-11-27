"use client";

import { useState } from "react";
import { MessageSquare, Pencil, Plus, Trash2 } from "@multica/ui/icons";
import {
  AGENT_CONVERSATION_STARTER_LABEL_MAX_LENGTH,
  AGENT_CONVERSATION_STARTER_MAX_LENGTH,
  AGENT_CONVERSATION_STARTERS_MAX,
} from "@multica/core/agents";
import type { AgentConversationStarter } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { useFallbackConversationStarters } from "../../chat/components/conversation-starter-list";
import { useT } from "../../i18n";

/**
 * A compact list of an agent's conversation starters: one summary row per
 * starter, opened in place to edit. With nothing configured it says the
 * built-in suggestions are in use instead of showing an empty list, because
 * that is what a new chat shows and what any starter added here replaces.
 */
export function ConversationStartersEditor({
  value,
  onChange,
  disabled = false,
}: {
  value: AgentConversationStarter[];
  onChange: (value: AgentConversationStarter[]) => void;
  disabled?: boolean;
}) {
  const { t } = useT("agents");
  const fallbackCount = useFallbackConversationStarters().length;
  const [editing, setEditing] = useState<number | null>(null);
  const hasIncompletePrompt = value.some(isIncomplete);
  // An open suggestion stays open until it is filled in or removed, so a
  // half-written one is never tucked away behind a collapsed row.
  const editingItem = editing === null ? undefined : value[editing];
  const editingIncomplete = editingItem ? isIncomplete(editingItem) : false;

  const update = (
    index: number,
    field: keyof AgentConversationStarter,
    nextValue: string,
  ) => {
    onChange(
      value.map((item, itemIndex) =>
        itemIndex === index ? { ...item, [field]: nextValue } : item,
      ),
    );
  };
  const add = () => {
    onChange([...value, { label: "", prompt: "" }]);
    setEditing(value.length);
  };
  const remove = (index: number) => {
    onChange(value.filter((_, itemIndex) => itemIndex !== index));
    setEditing(null);
  };

  if (value.length === 0) {
    return (
      <div className="flex min-h-10 items-center gap-2 rounded-lg border border-dashed py-1.5 pr-1.5 pl-3">
        <MessageSquare
          className="size-3.5 shrink-0 text-muted-foreground"
          aria-hidden="true"
        />
        <span className="min-w-0 flex-1 text-label text-muted-foreground">
          {t(($) => $.conversation_starters.defaults_summary, {
            count: fallbackCount,
          })}
        </span>
        <Button
          type="button"
          variant="outline"
          size="xs"
          disabled={disabled}
          onClick={add}
        >
          <Plus aria-hidden="true" />
          {t(($) => $.conversation_starters.customize)}
        </Button>
      </div>
    );
  }

  return (
    <div className="space-y-1.5">
      {value.map((item, index) =>
        editing === index ? (
          <div key={index} className="rounded-lg border border-ring/60 bg-card p-2.5">
            <Input
              value={item.label}
              maxLength={AGENT_CONVERSATION_STARTER_LABEL_MAX_LENGTH}
              disabled={disabled}
              autoFocus
              aria-label={t(($) => $.conversation_starters.item_label, {
                number: index + 1,
              })}
              placeholder={t(($) => $.conversation_starters.label_placeholder)}
              onChange={(event) => update(index, "label", event.target.value)}
            />
            <Textarea
              value={item.prompt}
              maxLength={AGENT_CONVERSATION_STARTER_MAX_LENGTH}
              disabled={disabled}
              rows={3}
              className="mt-2 resize-y"
              aria-label={t(($) => $.conversation_starters.prompt_label, {
                number: index + 1,
              })}
              placeholder={t(($) => $.conversation_starters.prompt_placeholder)}
              onChange={(event) => update(index, "prompt", event.target.value)}
            />
            <div className="mt-2 flex items-center justify-between gap-2">
              <Button
                type="button"
                variant="ghost"
                size="xs"
                className="text-destructive hover:text-destructive"
                disabled={disabled}
                aria-label={t(($) => $.conversation_starters.remove, {
                  number: index + 1,
                })}
                onClick={() => remove(index)}
              >
                <Trash2 aria-hidden="true" />
                {t(($) => $.conversation_starters.remove_short)}
              </Button>
              <Button
                type="button"
                variant="outline"
                size="xs"
                disabled={editingIncomplete}
                onClick={() => setEditing(null)}
              >
                {t(($) => $.conversation_starters.done)}
              </Button>
            </div>
          </div>
        ) : (
          <div
            key={index}
            className="flex items-center gap-1 rounded-lg border bg-card py-1.5 pr-1.5 pl-3"
          >
            <div className="min-w-0 flex-1">
              <p className="truncate text-label font-medium">
                {item.label.trim() || (
                  <span className="text-muted-foreground">
                    {t(($) => $.conversation_starters.untitled)}
                  </span>
                )}
              </p>
              {item.prompt.trim() ? (
                <p className="truncate text-caption text-muted-foreground">
                  {item.prompt}
                </p>
              ) : null}
            </div>
            <IconButton
              type="button"
              variant="ghost"
              size="icon-sm"
              disabled={disabled || editingIncomplete}
              label={t(($) => $.conversation_starters.edit, {
                number: index + 1,
              })}
              onClick={() => setEditing(index)}
            >
              <Pencil className="size-3.5" aria-hidden="true" />
            </IconButton>
          </div>
        ),
      )}

      <div className="flex items-center justify-between gap-2">
        {value.length < AGENT_CONVERSATION_STARTERS_MAX ? (
          <Button
            type="button"
            variant="ghost"
            size="xs"
            className="-ml-1.5 text-muted-foreground"
            disabled={disabled || editingIncomplete}
            onClick={add}
          >
            <Plus aria-hidden="true" />
            {t(($) => $.conversation_starters.add)}
          </Button>
        ) : (
          <span />
        )}
        <span className="text-caption tabular-nums text-muted-foreground">
          {value.length} / {AGENT_CONVERSATION_STARTERS_MAX}
        </span>
      </div>

      {hasIncompletePrompt ? (
        <p className="text-caption text-destructive" role="alert">
          {t(($) => $.conversation_starters.incomplete)}
        </p>
      ) : null}
    </div>
  );
}

function isIncomplete(item: AgentConversationStarter) {
  return !item.label.trim() || !item.prompt.trim();
}
