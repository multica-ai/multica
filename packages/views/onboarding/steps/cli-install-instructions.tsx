"use client";

import { useState, type ReactNode } from "react";
import { Check, Copy, Terminal } from "lucide-react";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { CODE_LIGATURE_CLASS } from "@multica/ui/lib/code-style";
import { cn } from "@multica/ui/lib/utils";
import { copyText } from "@multica/ui/lib/clipboard";
import { CliInstallCommand } from "../../common/cli-install-command";
import { useT } from "../../i18n";

const SETUP_CMD = "multica setup";

function CopyButton({ text }: { text: string }) {
  const { t } = useT("onboarding");
  const [copied, setCopied] = useState(false);

  const handleCopy = () => {
    void copyText(text).then((ok) => {
      if (!ok) return;
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    });
  };

  return (
    <button
      type="button"
      onClick={handleCopy}
      className="shrink-0 rounded-xs p-1 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
      aria-label={t(($) => $.cli_install.copy_aria)}
    >
      {copied ? (
        <Check className="h-3.5 w-3.5 text-success" />
      ) : (
        <Copy className="h-3.5 w-3.5" />
      )}
    </button>
  );
}

function CommandRow({ cmd }: { cmd: string }) {
  return (
    <div className="flex items-start gap-2 rounded-lg bg-muted px-3 py-2.5 font-mono text-body">
      <Terminal className="mt-0.5 h-3.5 w-3.5 shrink-0 text-muted-foreground" />
      <code
        className={cn(
          "min-w-0 flex-1 whitespace-pre-wrap break-all",
          CODE_LIGATURE_CLASS,
        )}
      >
        {cmd}
      </code>
      <CopyButton text={cmd} />
    </div>
  );
}

function Step({
  n,
  label,
  children,
}: {
  n: number;
  label: string;
  children: ReactNode;
}) {
  return (
    <div>
      <p className="mb-1.5 text-caption font-medium text-foreground">
        {n}. {label}
      </p>
      {children}
    </div>
  );
}

/**
 * CLI install instructions — two copy-and-run commands. Step 1 is the public
 * install script, which differs per OS and so renders through
 * `CliInstallCommand`'s platform switch; step 2 is the cloud
 * `multica setup`, hardcoded because the CLI itself knows the endpoints for
 * it. Local development tests a self-host variant by typing the extended
 * command directly in the terminal; no need to thread env vars through React.
 */
export function CliInstallInstructions() {
  const { t } = useT("onboarding");
  return (
    <Card className="w-full">
      <CardContent className="space-y-4 pt-4">
        <p className="text-caption leading-[1.55] text-muted-foreground">
          {t(($) => $.cli_install.intro)}
        </p>
        <Step n={1} label={t(($) => $.cli_install.step1_label)}>
          <CliInstallCommand
            labels={{
              group: t(($) => $.cli_install.platform_group),
              macosLinux: t(($) => $.cli_install.platform_macos_linux),
              windows: t(($) => $.cli_install.platform_windows),
            }}
          >
            {(cmd) => <CommandRow cmd={cmd} />}
          </CliInstallCommand>
        </Step>
        <Step n={2} label={t(($) => $.cli_install.step2_label)}>
          <CommandRow cmd={SETUP_CMD} />
        </Step>
      </CardContent>
    </Card>
  );
}
