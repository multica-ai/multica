"use client";

import { useState } from "react";
import { Check, Copy, Terminal } from "lucide-react";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { CODE_LIGATURE_CLASS } from "@multica/ui/lib/code-style";
import { cn } from "@multica/ui/lib/utils";
import { copyText } from "@multica/ui/lib/clipboard";
import { useT } from "../../i18n";
import { SegmentedToggle } from "../../common/segmented-toggle";
import {
  CONNECTOR_INSTALL_COMMANDS,
  CONNECTOR_SETUP_COMMAND,
  type ConnectorPlatform,
} from "../../common/connector-install";

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
      className="shrink-0 rounded p-1 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
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

function Step({ n, label, cmd }: { n: number; label: string; cmd: string }) {
  return (
    <div>
      <p className="mb-1.5 text-caption font-medium text-foreground">
        {n}. {label}
      </p>
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
    </div>
  );
}

/**
 * CLI install instructions — two copy-and-run commands. The selected platform
 * changes only the installer syntax; `multica setup` carries the fixed
 * deployment endpoint for both operating-system families.
 */
export function CliInstallInstructions() {
  const { t } = useT("onboarding");
  const [platform, setPlatform] = useState<ConnectorPlatform>("unix");
  return (
    <Card className="w-full">
      <CardContent className="space-y-4 pt-4">
        <p className="text-caption leading-[1.55] text-muted-foreground">
          {t(($) => $.cli_install.intro)}
        </p>
        <div>
          <p className="mb-1.5 text-caption font-medium text-foreground">
            {t(($) => $.cli_install.platform_label)}
          </p>
          <SegmentedToggle
            value={platform}
            onChange={setPlatform}
            ariaLabel={t(($) => $.cli_install.platform_label)}
            options={[
              ["unix", t(($) => $.cli_install.platform_unix)],
              ["windows", t(($) => $.cli_install.platform_windows)],
            ]}
            buttonClassName="px-3 py-1.5 text-caption"
          />
        </div>
        <Step
          n={1}
          label={t(($) => $.cli_install.step1_label)}
          cmd={CONNECTOR_INSTALL_COMMANDS[platform]}
        />
        <Step
          n={2}
          label={t(($) => $.cli_install.step2_label)}
          cmd={CONNECTOR_SETUP_COMMAND}
        />
      </CardContent>
    </Card>
  );
}
