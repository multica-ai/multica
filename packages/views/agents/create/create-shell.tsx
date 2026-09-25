"use client";

import type { ReactNode } from "react";
import { ArrowLeft } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../i18n";

/**
 * Chrome shared by every agent-creation route: the back control, the flow
 * title, and the current step. Each route owns what "back" means — the chooser
 * leaves the flow, the two method routes return to the chooser — so the shell
 * takes a handler rather than deriving it.
 */
export function AgentCreateShell({
  title,
  step,
  onBack,
  backDisabled = false,
  chips,
  children,
}: {
  title: string;
  step: string;
  onBack: () => void;
  backDisabled?: boolean;
  /** Right-aligned context badges (method, runtime). Hidden on small screens. */
  chips?: ReactNode;
  children: ReactNode;
}) {
  const { t } = useT("agents");
  return (
    <div className="flex min-h-0 flex-1 flex-col bg-background">
      <header className="flex h-14 shrink-0 items-center gap-3 border-b px-5">
        <Button
          type="button"
          variant="ghost"
          size="icon"
          onClick={onBack}
          disabled={backDisabled}
          aria-label={t(($) => $.creation_studio.back)}
        >
          <ArrowLeft className="size-4" aria-hidden="true" />
        </Button>
        <div className="min-w-0">
          <h1 className="truncate text-body font-semibold">{title}</h1>
          <p className="truncate text-caption text-muted-foreground">{step}</p>
        </div>
        {chips ? (
          <div className="ml-auto hidden items-center gap-2 text-caption text-muted-foreground sm:flex">
            {chips}
          </div>
        ) : null}
      </header>
      {children}
    </div>
  );
}

export function AgentCreateChip({ children }: { children: ReactNode }) {
  return <span className="rounded-full bg-muted px-2 py-1">{children}</span>;
}
