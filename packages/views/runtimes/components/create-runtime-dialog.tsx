"use client";

import { useState } from "react";
import { Cloud, Monitor, Terminal, Loader2, ArrowLeft, RefreshCw } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@multica/ui/components/ui/dialog";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCreateSandboxConfig } from "@multica/core/workspace/mutations";
import type { SandboxProvider, SandboxTemplate } from "@multica/core/types";

type Step = "choose" | "cloud" | "local";

export function CreateRuntimeDialog({ onClose }: { onClose: () => void }) {
  const [step, setStep] = useState<Step>("choose");

  return (
    <Dialog open onOpenChange={(v) => { if (!v) onClose(); }}>
      <DialogContent className="sm:max-w-md">
        {step === "choose" && <ChooseStep onSelect={setStep} onClose={onClose} />}
        {step === "cloud" && <CloudStep onBack={() => setStep("choose")} onClose={onClose} />}
        {step === "local" && <LocalStep onBack={() => setStep("choose")} onClose={onClose} />}
      </DialogContent>
    </Dialog>
  );
}

function ChooseStep({
  onSelect,
  onClose,
}: {
  onSelect: (step: Step) => void;
  onClose: () => void;
}) {
  return (
    <>
      <DialogHeader>
        <DialogTitle>Add Runtime</DialogTitle>
        <DialogDescription>
          Choose how the runtime will execute agent tasks.
        </DialogDescription>
      </DialogHeader>

      <div className="grid grid-cols-2 gap-3">
        <button
          type="button"
          onClick={() => onSelect("cloud")}
          className="flex flex-col items-center gap-2 rounded-lg border border-border p-4 text-center transition-colors hover:border-primary hover:bg-primary/5"
        >
          <Cloud className="h-6 w-6 text-muted-foreground" />
          <div>
            <div className="text-sm font-medium">Cloud</div>
            <div className="mt-0.5 text-xs text-muted-foreground">
              Remote sandbox (E2B)
            </div>
          </div>
        </button>
        <button
          type="button"
          onClick={() => onSelect("local")}
          className="flex flex-col items-center gap-2 rounded-lg border border-border p-4 text-center transition-colors hover:border-primary hover:bg-primary/5"
        >
          <Monitor className="h-6 w-6 text-muted-foreground" />
          <div>
            <div className="text-sm font-medium">Local</div>
            <div className="mt-0.5 text-xs text-muted-foreground">
              Your machine via CLI
            </div>
          </div>
        </button>
      </div>

      <DialogFooter>
        <Button variant="ghost" onClick={onClose}>Cancel</Button>
      </DialogFooter>
    </>
  );
}

function CloudStep({
  onBack,
  onClose,
}: {
  onBack: () => void;
  onClose: () => void;
}) {
  const wsId = useWorkspaceId();
  const create = useCreateSandboxConfig(wsId);

  const [name, setName] = useState("");
  const [provider, setProvider] = useState<SandboxProvider>("e2b");
  const [apiKey, setApiKey] = useState("");
  const [aiGatewayKey, setAiGatewayKey] = useState("");
  const [gitPat, setGitPat] = useState("");
  const [templateId, setTemplateId] = useState("");
  const [templates, setTemplates] = useState<SandboxTemplate[]>([]);
  const [loadingTemplates, setLoadingTemplates] = useState(false);

  const loadTemplates = async () => {
    if (!apiKey.trim()) return;
    setLoadingTemplates(true);
    try {
      const data = await (await import("@multica/core/api")).api.listTemplatesByKey(apiKey.trim());
      setTemplates(data);
    } catch {
      toast.error("Failed to load templates");
    } finally {
      setLoadingTemplates(false);
    }
  };

  const handleSubmit = () => {
    if (!apiKey.trim()) {
      toast.error("Provider API key is required");
      return;
    }
    create.mutate(
      {
        name: name.trim() || `Cloud Runtime (${provider})`,
        provider,
        provider_api_key: apiKey.trim(),
        ai_gateway_api_key: aiGatewayKey.trim() || undefined,
        git_pat: gitPat.trim() || undefined,
        template_id: templateId || undefined,
      },
      {
        onSuccess: () => {
          toast.success("Cloud runtime created");
          onClose();
        },
        onError: (e) => toast.error(e instanceof Error ? e.message : "Failed to create"),
      },
    );
  };

  return (
    <>
      <DialogHeader>
        <DialogTitle className="flex items-center gap-2">
          <button type="button" onClick={onBack} className="rounded-md p-0.5 hover:bg-accent">
            <ArrowLeft className="h-4 w-4" />
          </button>
          Cloud Runtime
        </DialogTitle>
        <DialogDescription>
          Configure a sandbox provider for cloud agent execution.
        </DialogDescription>
      </DialogHeader>

      <div className="space-y-3">
        <div>
          <Label className="text-xs text-muted-foreground">Name</Label>
          <Input
            autoFocus
            type="text"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="e.g. My E2B Sandbox"
            className="mt-1"
          />
        </div>

        <div>
          <Label className="text-xs text-muted-foreground">Provider</Label>
          <select
            value={provider}
            onChange={(e) => setProvider(e.target.value as SandboxProvider)}
            className="mt-1 flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-sm transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
          >
            <option value="e2b">E2B</option>
            <option value="daytona" disabled>Daytona (coming soon)</option>
          </select>
        </div>

        <div>
          <Label className="text-xs text-muted-foreground">Provider API Key *</Label>
          <div className="mt-1 flex gap-2">
            <Input
              type="password"
              value={apiKey}
              onChange={(e) => setApiKey(e.target.value)}
              placeholder="sk-e2b-..."
              onBlur={loadTemplates}
            />
            <Button
              type="button"
              variant="outline"
              size="icon"
              onClick={loadTemplates}
              disabled={loadingTemplates || !apiKey.trim()}
              title="Load templates"
            >
              {loadingTemplates ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <RefreshCw className="h-3.5 w-3.5" />
              )}
            </Button>
          </div>
        </div>

        <div>
          <Label className="text-xs text-muted-foreground">Template</Label>
          {templates.length > 0 ? (
            <select
              value={templateId}
              onChange={(e) => setTemplateId(e.target.value)}
              className="mt-1 flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-sm transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
            >
              <option value="">base (default)</option>
              {templates.map((t) => (
                <option key={t.template_id} value={t.template_id}>
                  {t.name} ({t.cpu_count} vCPU, {t.memory_mb}MB)
                </option>
              ))}
            </select>
          ) : (
            <Input
              value={templateId}
              onChange={(e) => setTemplateId(e.target.value)}
              placeholder="Enter API key first to load templates"
              className="mt-1"
            />
          )}
        </div>

        <div>
          <Label className="text-xs text-muted-foreground">AI Gateway API Key</Label>
          <Input
            type="password"
            value={aiGatewayKey}
            onChange={(e) => setAiGatewayKey(e.target.value)}
            placeholder="Optional"
            className="mt-1"
          />
          <p className="mt-0.5 text-xs text-muted-foreground">
            For model access inside the sandbox.
          </p>
        </div>

        <div>
          <Label className="text-xs text-muted-foreground">Git Personal Access Token</Label>
          <Input
            type="password"
            value={gitPat}
            onChange={(e) => setGitPat(e.target.value)}
            placeholder="Optional"
            className="mt-1"
          />
          <p className="mt-0.5 text-xs text-muted-foreground">
            Enables agents to push code and create PRs.
          </p>
        </div>
      </div>

      <DialogFooter>
        <Button variant="ghost" onClick={onBack}>Back</Button>
        <Button onClick={handleSubmit} disabled={create.isPending || !apiKey.trim()}>
          {create.isPending ? (
            <>
              <Loader2 className="h-3 w-3 animate-spin" />
              Creating...
            </>
          ) : (
            "Create"
          )}
        </Button>
      </DialogFooter>
    </>
  );
}

function LocalStep({
  onBack,
  onClose,
}: {
  onBack: () => void;
  onClose: () => void;
}) {
  return (
    <>
      <DialogHeader>
        <DialogTitle className="flex items-center gap-2">
          <button type="button" onClick={onBack} className="rounded-md p-0.5 hover:bg-accent">
            <ArrowLeft className="h-4 w-4" />
          </button>
          Local Runtime
        </DialogTitle>
        <DialogDescription>
          Run an agent runtime on your local machine using the Multica CLI.
        </DialogDescription>
      </DialogHeader>

      <div className="space-y-3">
        <div className="rounded-lg border bg-muted/50 p-4 space-y-3">
          <div className="flex items-center gap-2 text-sm font-medium">
            <Terminal className="h-4 w-4 text-muted-foreground" />
            Getting started
          </div>
          <ol className="space-y-2 text-sm text-muted-foreground">
            <li className="flex gap-2">
              <span className="shrink-0 font-medium text-foreground">1.</span>
              <span>
                Install the CLI:{" "}
                <code className="rounded bg-muted px-1.5 py-0.5 text-xs">
                  brew install multica-ai/tap/multica
                </code>
              </span>
            </li>
            <li className="flex gap-2">
              <span className="shrink-0 font-medium text-foreground">2.</span>
              <span>
                Log in:{" "}
                <code className="rounded bg-muted px-1.5 py-0.5 text-xs">
                  multica auth login
                </code>
              </span>
            </li>
            <li className="flex gap-2">
              <span className="shrink-0 font-medium text-foreground">3.</span>
              <span>
                Start the daemon:{" "}
                <code className="rounded bg-muted px-1.5 py-0.5 text-xs">
                  multica daemon start
                </code>
              </span>
            </li>
          </ol>
          <p className="text-xs text-muted-foreground">
            The runtime will auto-register once the daemon connects.
          </p>
        </div>
      </div>

      <DialogFooter>
        <Button variant="ghost" onClick={onBack}>Back</Button>
        <Button onClick={onClose}>Done</Button>
      </DialogFooter>
    </>
  );
}
