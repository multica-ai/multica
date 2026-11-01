"use client";

import { Fragment, type ReactNode } from "react";
import { Check, X } from "@multica/ui/icons";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { Separator } from "@multica/ui/components/ui/separator";
import { cn } from "@multica/ui/lib/utils";
import { PageHeader } from "../../layout/page-header";
import { useT } from "../../i18n";

export type AgentCreateStep = 1 | 2 | 3;

export const AGENT_CREATE_STEP_COUNT = 3;

/**
 * Chrome shared by every agent-creation route: the close control, the flow
 * title, and where the route sits in runtime → configure → done. Each route
 * owns what leaving means, so the shell takes a handler rather than deriving
 * it.
 */
export function AgentCreateShell({
  title,
  step,
  stepIndex,
  onClose,
  chips,
  children,
}: {
  title: string;
  /** Names the screen where there is no step position to show. */
  step: string;
  /** Position in runtime → configure → done. Omitted by off-path routes. */
  stepIndex?: AgentCreateStep;
  onClose: () => void;
  /** Right-aligned context badges (method, runtime). Hidden on small screens. */
  chips?: ReactNode;
  children: ReactNode;
}) {
  const { t } = useT("agents");
  return (
    <div className="flex min-h-0 flex-1 flex-col bg-background">
      <PageHeader
        leading={
          <IconButton
            label={t(($) => $.creation_studio.close)}
            tooltipSide="bottom"
            className="text-muted-foreground"
            onClick={onClose}
          >
            <X />
          </IconButton>
        }
      >
        <div className="flex min-w-0 flex-1 items-center gap-3">
          <h1 className="min-w-0 truncate text-body font-medium">{title}</h1>
          <Separator
            orientation="vertical"
            className="data-vertical:h-4 data-vertical:self-center"
          />
          {stepIndex ? (
            <CreateStepper active={stepIndex} />
          ) : (
            <p className="min-w-0 truncate text-body text-muted-foreground">{step}</p>
          )}
        </div>
        {chips ? (
          <div className="hidden shrink-0 items-center gap-2 text-caption text-muted-foreground sm:flex">
            {chips}
          </div>
        ) : null}
      </PageHeader>
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
      className="flex min-w-0 items-center gap-2 text-body text-muted-foreground"
      aria-label={t(($) => $.creation_studio.steps.aria)}
    >
      {labels.map((label, index) => {
        const number = index + 1;
        const done = number < active;
        const current = number === active;
        return (
          <Fragment key={label}>
            {index > 0 ? (
              <li aria-hidden="true" className="hidden h-px w-8 bg-border md:block" />
            ) : null}
            <li
              aria-current={current ? "step" : undefined}
              className={cn(
                "flex min-w-0 items-center gap-1.5",
                current ? "font-medium text-foreground" : "hidden md:flex",
              )}
            >
              <span
                className={cn(
                  "flex size-[18px] shrink-0 items-center justify-center rounded-full border text-micro tabular-nums",
                  current && "border-primary bg-primary text-primary-foreground",
                  done && "border-success/30 bg-success/10 text-success",
                )}
              >
                {done ? <Check className="size-3" aria-hidden="true" /> : number}
              </span>
              <span className="truncate">{label}</span>
            </li>
          </Fragment>
        );
      })}
    </ol>
  );
}
