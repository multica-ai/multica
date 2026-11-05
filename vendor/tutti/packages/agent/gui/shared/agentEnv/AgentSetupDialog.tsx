import type { ReactNode } from "react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  LoadingIcon,
  SuccessFilledIcon,
  WarningFilledIcon
} from "@tutti-os/ui-system";
import { observer } from "mobx-react-lite";
export const AgentSetupStepIcon = observer(AgentSetupStepIconRender);
export const AgentSetupDialog = observer(AgentSetupDialogRender);

export type AgentSetupStepStatus =
  | "pending"
  | "running"
  | "ok"
  | "error"
  | "skipped";

function AgentSetupStepIconRender({
  status,
  warning = false
}: {
  status: AgentSetupStepStatus;
  warning?: boolean;
}): React.JSX.Element {
  if (status === "ok") {
    return <SuccessFilledIcon className="size-4 text-[var(--tutti-purple)]" />;
  }
  if (status === "running") {
    return <LoadingIcon className="size-4 animate-spin" />;
  }
  if (status === "error") {
    return (
      <WarningFilledIcon
        className={`size-4 ${warning ? "text-[var(--state-warning)]" : "text-[var(--state-danger)]"}`}
      />
    );
  }
  return (
    <span
      aria-hidden="true"
      className="size-4 rounded-full border border-[var(--border-1)]"
    />
  );
}

export interface AgentSetupDialogProps {
  afterContent?: ReactNode;
  children: ReactNode;
  description: ReactNode;
  footer?: ReactNode;
  onOpenChange: (open: boolean) => void;
  open: boolean;
  title: ReactNode;
}

function AgentSetupDialogRender({
  afterContent,
  children,
  description,
  footer,
  onOpenChange,
  open,
  title
}: AgentSetupDialogProps): React.JSX.Element {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="nodrag tsh-desktop-no-drag flex max-h-[min(640px,calc(100vh-32px))] flex-col gap-0 overflow-hidden bg-[var(--background-fronted)] p-0 [-webkit-app-region:no-drag] sm:max-w-[560px]">
        <DialogHeader className="shrink-0 border-b border-[var(--border-1)] px-5 py-4">
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>

        <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">
          {children}
        </div>

        {afterContent}

        {footer ? (
          <DialogFooter className="flex shrink-0 items-center justify-end gap-2 border-t border-[var(--border-1)] px-5 py-4">
            {footer}
          </DialogFooter>
        ) : null}
      </DialogContent>
    </Dialog>
  );
}
