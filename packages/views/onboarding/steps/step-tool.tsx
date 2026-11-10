"use client";

import { useState } from "react";
import { Loader2 } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import type { AgentRuntime } from "@multica/core/types";
import { useT } from "../../i18n";
import { StepFooter, StepHeading } from "../components/step-shell";
import { ToolPicker, type ToolSelection } from "../components/tool-picker";
import { toolDisplayName } from "../provisioning";

/**
 * Pick the coding tool that becomes the first agent. Nothing is created here:
 * the agent is made together with the first task, so walking back and picking
 * another tool never leaves a stray agent behind.
 */
export function StepTool({
  runtimes,
  detecting,
  canRescan,
  onRescan,
  workspaceSlug,
  value,
  onChange,
  onContinue,
  onSkip,
}: {
  runtimes: AgentRuntime[];
  detecting: boolean;
  canRescan: boolean;
  onRescan: () => Promise<void>;
  workspaceSlug?: string;
  value: ToolSelection;
  onChange: (next: ToolSelection) => void;
  onContinue: () => void;
  /** Leave onboarding without an agent, for someone with no tool to hand. */
  onSkip: () => Promise<void>;
}) {
  const { t } = useT("onboarding");
  const [skipping, setSkipping] = useState(false);
  const selected = runtimes.find((r) => r.id === value.runtimeId) ?? null;
  const empty = !detecting && runtimes.length === 0;

  const skip = async () => {
    setSkipping(true);
    try {
      await onSkip();
    } finally {
      setSkipping(false);
    }
  };

  return (
    <>
      <div className="flex flex-col gap-6 pt-6">
        <StepHeading
          title={t(($) => $.step_tool.title)}
          description={
            empty ? t(($) => $.step_tool.description_empty) : t(($) => $.step_tool.description)
          }
        />
        <ToolPicker
          runtimes={runtimes}
          detecting={detecting}
          canRescan={canRescan}
          onRescan={onRescan}
          workspaceSlug={workspaceSlug}
          value={value}
          onChange={onChange}
          disabled={skipping}
        />
      </div>

      <StepFooter hint={selected ? t(($) => $.step_tool.hint) : undefined}>
        <Button className="w-full" disabled={!selected || skipping} onClick={onContinue}>
          {selected
            ? t(($) => $.step_tool.continue_with, { name: toolDisplayName(selected.provider) })
            : t(($) => $.common.continue)}
        </Button>
        <Button
          variant="ghost"
          className="w-full text-muted-foreground"
          disabled={skipping}
          aria-busy={skipping}
          onClick={skip}
        >
          {skipping ? <Loader2 className="animate-spin" /> : null}
          {t(($) => $.step_tool.skip)}
        </Button>
      </StepFooter>
    </>
  );
}
