"use client";

import { Fragment, type ReactNode } from "react";
import { ArrowLeft, Check, X } from "@multica/ui/icons";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

export type AgentCreateStep = 1 | 2 | 3;

/**
 * Chrome shared by every agent-creation route: the back control, the flow
 * title, and the current step. Each route owns what "back" means, so the shell
 * takes a handler rather than deriving it.
 */
export function AgentCreateShell({
  title,
  step,
  stepIndex,
  onBack,
  backDisabled = false,
  closeIcon = false,
  chips,
  children,
}: {
  title: string;
  step: string;
  /** Position in runtime → configure → done. Omitted by off-path routes. */
  stepIndex?: AgentCreateStep;
  onBack: () => void;
  backDisabled?: boolean;
  /** The flow is finished, so the control leaves it instead of going back. */
  closeIcon?: boolean;
  /** Right-aligned context badges (method, runtime). Hidden on small screens. */
  chips?: ReactNode;
  children: ReactNode;
}) {
  const { t } = useT("agents");
  return (
    <div className="flex min-h-0 flex-1 flex-col bg-background">
      <header className="flex h-14 shrink-0 items-center gap-3 border-b px-5">
        <IconButton
          type="button"
          variant="ghost"
          size="icon"
          onClick={onBack}
          disabled={backDisabled}
          label={
            closeIcon
              ? t(($) => $.creation_studio.close)
              : t(($) => $.creation_studio.back)
          }
          tooltipSide="bottom"
        >
          {closeIcon ? (
            <X className="size-4" aria-hidden="true" />
          ) : (
            <ArrowLeft className="size-4" aria-hidden="true" />
          )}
        </IconButton>
        <div className="min-w-0">
          <h1 className="truncate text-body font-semibold">{title}</h1>
          <p className="truncate text-caption text-muted-foreground">{step}</p>
        </div>
        {chips || stepIndex ? (
          <div className="ml-auto hidden items-center gap-4 text-caption text-muted-foreground sm:flex">
            {chips ? <div className="flex items-center gap-2">{chips}</div> : null}
            {stepIndex ? <CreateStepper active={stepIndex} /> : null}
          </div>
        ) : null}
      </header>
      {children}
    </div>
  );
}

export function AgentCreateChip({ children }: { children: ReactNode }) {
  return (
    <span className="flex max-w-64 items-center gap-1.5 truncate rounded-full bg-muted px-2 py-1">
      {children}
    </span>
  );
}

function CreateStepper({ active }: { active: AgentCreateStep }) {
  const { t } = useT("agents");
  const labels = [
    t(($) => $.creation_studio.steps.runtime),
    t(($) => $.creation_studio.steps.configure),
    t(($) => $.creation_studio.steps.done),
  ];
  return (
    <ol
      className="hidden items-center gap-2 lg:flex"
      aria-label={t(($) => $.creation_studio.steps.aria)}
    >
      {labels.map((label, index) => {
        const number = index + 1;
        const done = number < active;
        const current = number === active;
        return (
          <Fragment key={label}>
            {index > 0 ? (
              <li aria-hidden="true" className="h-px w-5 bg-border" />
            ) : null}
            <li
              aria-current={current ? "step" : undefined}
              className={cn(
                "flex items-center gap-1.5",
                current && "font-medium text-foreground",
              )}
            >
              <span
                className={cn(
                  "flex size-[18px] items-center justify-center rounded-full border text-micro tabular-nums",
                  current && "border-primary bg-primary text-primary-foreground",
                  done && "border-success/30 bg-success/10 text-success",
                )}
              >
                {done ? <Check className="size-3" aria-hidden="true" /> : number}
              </span>
              {label}
            </li>
          </Fragment>
        );
      })}
    </ol>
  );
}
