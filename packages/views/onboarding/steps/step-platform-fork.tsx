"use client";

import { useState, type ReactNode } from "react";
import {
  ArrowLeft,
  ArrowRight,
  Cloud,
  Download,
  Monitor,
  Terminal,
} from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import type { AgentRuntime } from "@multica/core/types";
import { StepRuntimeConnect } from "./step-runtime-connect";
import { StepCloudWaitlist } from "./step-cloud-waitlist";

type ForkState = "fork" | "downloading" | "cli" | "waitlist";

const DESKTOP_DOWNLOAD_URL = "https://multica.ai/download";

export function StepPlatformFork({
  wsId,
  onNext,
  onWaitlist,
  cliInstructions,
}: {
  wsId: string;
  onNext: (runtime: AgentRuntime | null) => void;
  onWaitlist: (email: string) => void;
  cliInstructions?: ReactNode;
}) {
  const [state, setState] = useState<ForkState>("fork");

  if (state === "cli") {
    return (
      <div className="flex w-full flex-col gap-4">
        <button
          type="button"
          onClick={() => setState("fork")}
          className="flex items-center gap-1.5 self-start text-sm text-muted-foreground hover:text-foreground"
        >
          <ArrowLeft className="h-3.5 w-3.5" />
          Back to options
        </button>
        <StepRuntimeConnect
          wsId={wsId}
          onNext={onNext}
          instructions={cliInstructions}
        />
      </div>
    );
  }

  if (state === "waitlist") {
    return (
      <StepCloudWaitlist
        onBack={() => setState("fork")}
        onSubmit={onWaitlist}
      />
    );
  }

  if (state === "downloading") {
    return (
      <div className="flex w-full flex-col items-center gap-6 text-center">
        <div className="flex h-12 w-12 items-center justify-center rounded-full bg-primary/10 text-primary">
          <Download className="h-6 w-6" />
        </div>
        <div className="flex flex-col gap-2">
          <h1 className="text-2xl font-semibold tracking-tight">
            Multica Desktop is downloading
          </h1>
          <p className="text-sm text-muted-foreground">
            Open the installer, then sign in with this account on the
            desktop app — your setup will continue there.
          </p>
        </div>
        <button
          type="button"
          onClick={() => setState("cli")}
          className="text-sm text-muted-foreground hover:text-foreground"
        >
          ← I&apos;d rather install the CLI
        </button>
      </div>
    );
  }

  return (
    <div className="flex w-full flex-col gap-6">
      <div className="flex flex-col gap-2 text-center">
        <h1 className="text-2xl font-semibold tracking-tight">
          Connect a runtime
        </h1>
        <p className="text-sm text-muted-foreground">
          Agents need a place to run. Multica Desktop has one built in — recommended.
        </p>
      </div>

      <div className="flex items-center gap-4 rounded-xl border border-primary/20 bg-primary/5 p-4">
        <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
          <Monitor className="h-5 w-5" />
        </div>
        <div className="min-w-0 flex-1">
          <div className="text-sm font-medium">Multica Desktop</div>
          <div className="text-xs text-muted-foreground">
            Bundled runtime, no terminal needed.
          </div>
        </div>
        <Button
          size="sm"
          onClick={() => {
            window.open(DESKTOP_DOWNLOAD_URL, "_blank", "noopener,noreferrer");
            setState("downloading");
          }}
        >
          <Download className="h-3.5 w-3.5" />
          Download
        </Button>
      </div>

      <div className="flex flex-col gap-1">
        <p className="px-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">
          Other ways to connect
        </p>
        <AlternativeRow
          icon={<Terminal className="h-4 w-4" />}
          label="Install the CLI instead"
          hint="2–4 min"
          onClick={() => setState("cli")}
        />
        <AlternativeRow
          icon={<Cloud className="h-4 w-4" />}
          label="Join the cloud waitlist"
          onClick={() => setState("waitlist")}
        />
      </div>
    </div>
  );
}

function AlternativeRow({
  icon,
  label,
  hint,
  onClick,
}: {
  icon: ReactNode;
  label: string;
  hint?: string;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="flex items-center gap-3 rounded-lg px-2 py-2.5 text-left text-sm transition-colors hover:bg-muted"
    >
      <span className="text-muted-foreground">{icon}</span>
      <span className="flex-1">{label}</span>
      {hint && <span className="text-xs text-muted-foreground">{hint}</span>}
      <ArrowRight className="h-3.5 w-3.5 text-muted-foreground" />
    </button>
  );
}
