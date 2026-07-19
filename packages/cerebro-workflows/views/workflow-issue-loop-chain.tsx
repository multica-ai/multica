import { Info } from "lucide-react";

import type { LoopChainSpec } from "../core/types";

export interface ChainFormState {
  chain: LoopChainSpec;
}

function cloneChain(chain: LoopChainSpec): LoopChainSpec {
  return JSON.parse(JSON.stringify(chain)) as LoopChainSpec;
}

export function chainFormFromSpec(spec: LoopChainSpec): ChainFormState {
  return { chain: cloneChain(spec) };
}

export function chainSpecFromForm(form: ChainFormState): LoopChainSpec {
  return cloneChain(form.chain);
}

export function hasMachineControl(chain: LoopChainSpec): boolean {
  return chain.phases.some((phase) =>
    phase.blocks.some((block) => block.type === "command" || block.type === "eval"),
  );
}

export function MachineControlNotice({ chain }: { chain: LoopChainSpec }) {
  if (hasMachineControl(chain)) return null;
  return (
    <div className="flex gap-2 rounded-md border border-warning/40 bg-warning/5 p-3 text-xs">
      <Info className="mt-0.5 size-4 shrink-0 text-warning" aria-hidden="true" />
      <div className="space-y-1">
        <p className="font-medium text-foreground">This chain has no machine-controlled block</p>
        <p className="text-muted-foreground">
          This is informational, not a blocker. Add a Command or Eval block when the
          result should be decided by the engine rather than a review or approval.
        </p>
      </div>
    </div>
  );
}
