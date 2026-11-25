"use client";

import { useRef, useState, type ReactNode, type SyntheticEvent } from "react";
import { Check } from "@multica/ui/icons";
import type { AgentRuntime } from "@multica/core/types";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  Popover,
  PopoverContent,
} from "@multica/ui/components/ui/popover";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import {
  MemberPicker,
  SCOPE_ICON,
  runtimeAccessScope,
  useRuntimeAccess,
  type RuntimeAccessScope,
} from "./runtime-access-editor";

const SCOPES: readonly RuntimeAccessScope[] = ["private", "public", "members"];

const MARK_CLASS =
  "-mx-1 inline-flex h-5 min-w-5 shrink-0 items-center justify-center gap-0.5 rounded-xs px-1 text-micro font-medium tabular-nums text-faint-foreground";

/**
 * A runtime's access scope at the right edge of its name cell, so the scopes
 * of every row line up. The owner changes it from a menu; everyone else,
 * admins included, reads the same hint as on the detail page.
 */
export function RuntimeListVisibility({
  runtime,
  canEdit,
}: {
  runtime: AgentRuntime;
  canEdit: boolean;
}) {
  const { t } = useT("runtimes");
  const scope = runtimeAccessScope(runtime);
  const Icon = SCOPE_ICON[scope];
  const count = runtime.shared_with?.length ?? 0;
  const label =
    scope === "members"
      ? t(($) => $.detail.visibility_members_count, { count })
      : t(($) => $.detail.visibility_label[scope]);
  const ariaLabel = `${t(($) => $.detail.diagnostics_visibility)}: ${label}`;
  const mark = (
    <>
      <Icon className="size-3" aria-hidden="true" />
      {scope === "members" ? <span aria-hidden="true">{count}</span> : null}
    </>
  );

  if (!canEdit) {
    return (
      <Tooltip>
        <TooltipTrigger
          render={
            <span role="img" aria-label={ariaLabel} className={MARK_CLASS}>
              {mark}
            </span>
          }
        />
        <TooltipContent>
          {label} · {t(($) => $.detail.visibility_hint_readonly[scope])}
        </TooltipContent>
      </Tooltip>
    );
  }

  return (
    <VisibilityMenu
      runtime={runtime}
      ariaLabel={ariaLabel}
      tooltip={`${label} · ${t(($) => $.detail.visibility_hint[scope])}`}
    >
      {mark}
    </VisibilityMenu>
  );
}

function VisibilityMenu({
  runtime,
  ariaLabel,
  tooltip,
  children,
}: {
  runtime: AgentRuntime;
  ariaLabel: string;
  tooltip: string;
  children: ReactNode;
}) {
  const { t } = useT("runtimes");
  const access = useRuntimeAccess(runtime);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const [menuOpen, setMenuOpen] = useState(false);
  const [pickerOpen, setPickerOpen] = useState(false);
  const [hover, setHover] = useState(false);

  const choose = (scope: RuntimeAccessScope) => {
    if (scope === "members") {
      setPickerOpen(true);
      return;
    }
    if (access.current !== scope) access.apply(scope, []);
  };

  // The trigger sits inside the row's link. Its menu, picker and dialog are
  // portaled, and the row already ignores events from outside its own DOM.
  const stop = (e: SyntheticEvent) => e.stopPropagation();

  return (
    <span className="inline-flex shrink-0" onClick={stop} onAuxClick={stop}>
      <DropdownMenu open={menuOpen} onOpenChange={setMenuOpen}>
        <Tooltip open={hover && !menuOpen && !pickerOpen} onOpenChange={setHover}>
          <TooltipTrigger
            render={
              <DropdownMenuTrigger
                ref={triggerRef}
                aria-label={ariaLabel}
                className={cn(
                  MARK_CLASS,
                  "cursor-pointer transition-colors hover:bg-accent hover:text-muted-foreground",
                  "data-popup-open:bg-accent data-popup-open:text-muted-foreground",
                  pickerOpen && "bg-accent text-muted-foreground",
                )}
              >
                {children}
              </DropdownMenuTrigger>
            }
          />
          <TooltipContent>{tooltip}</TooltipContent>
        </Tooltip>
        <DropdownMenuContent align="start" className="w-48">
          {SCOPES.map((scope) => {
            const Icon = SCOPE_ICON[scope];
            return (
              <DropdownMenuItem
                key={scope}
                disabled={access.disabled || (scope === "members" && access.membersDisabled)}
                onClick={() => choose(scope)}
              >
                <Icon className="text-muted-foreground" aria-hidden="true" />
                <span>
                  {t(($) => $.detail.visibility_label[scope])}
                  {scope === "members" ? "…" : null}
                </span>
                {access.current === scope ? (
                  <Check className="ml-auto size-3.5" aria-hidden="true" />
                ) : null}
              </DropdownMenuItem>
            );
          })}
        </DropdownMenuContent>
      </DropdownMenu>
      <Popover open={pickerOpen} onOpenChange={setPickerOpen}>
        <PopoverContent anchor={triggerRef} align="start" className="w-72 p-0">
          {pickerOpen ? (
            <MemberPicker
              members={access.candidates}
              initial={access.persisted}
              saving={access.saving}
              onCancel={() => setPickerOpen(false)}
              onSave={(ids) => access.apply("members", ids, () => setPickerOpen(false))}
            />
          ) : null}
        </PopoverContent>
      </Popover>
      {access.revokeDialog}
    </span>
  );
}
