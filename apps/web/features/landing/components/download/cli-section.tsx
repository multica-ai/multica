"use client";

import { useState, type ReactNode } from "react";
import { Check, Copy, Terminal } from "lucide-react";
import { CliInstallCommand } from "@multica/views/common/cli-install-command";
import { copyText } from "@multica/ui/lib/clipboard";
import { useLocale } from "../../i18n";

const SETUP_CMD = "multica setup";

/**
 * The landing palette bypasses the product tokens, so the shared platform
 * switch takes its list and trigger colours from here.
 */
const PLATFORM_TABS = {
  list: "border border-[#0a0d12]/10 bg-white",
  trigger:
    "text-[#0a0d12]/60 hover:text-[#0a0d12] data-active:bg-[#0a0d12]/5 data-active:text-[#0a0d12]",
};

/**
 * Scenario-first CLI section. Copy leans into servers / remote dev
 * boxes / headless setups rather than positioning CLI as a
 * lightweight Desktop. Two copy-and-paste command blocks; the install one
 * carries the platform switch, since install.sh and install.ps1 differ.
 */
export function CliSection() {
  const { t } = useLocale();
  const d = t.download.cli;

  return (
    <section id="cli" className="bg-[#f7f7f5] py-20 text-[#0a0d12] sm:py-24">
      <div className="mx-auto max-w-[820px] px-4 sm:px-6 lg:px-8">
        <h2 className="landing-serif text-[2.2rem] leading-[1.1] tracking-[-0.03em] sm:text-[2.6rem]">
          {d.title}
        </h2>
        <p className="mt-4 max-w-[620px] text-body-lg leading-7 text-[#0a0d12]/72">
          {d.sub}
        </p>

        <div className="mt-10 flex flex-col gap-5">
          <div>
            <CommandLabel>{d.installLabel}</CommandLabel>
            <CliInstallCommand
              labels={{
                group: d.platformGroup,
                macosLinux: d.platformMacosLinux,
                windows: d.platformWindows,
              }}
              classNames={PLATFORM_TABS}
            >
              {(cmd) => (
                <CommandRow
                  cmd={cmd}
                  copyLabel={d.copyLabel}
                  copiedLabel={d.copiedLabel}
                />
              )}
            </CliInstallCommand>
          </div>
          <div>
            <CommandLabel>{d.startLabel}</CommandLabel>
            <CommandRow
              cmd={SETUP_CMD}
              copyLabel={d.copyLabel}
              copiedLabel={d.copiedLabel}
            />
          </div>
        </div>

        <p className="mt-6 text-label text-[#0a0d12]/60">{d.sshNote}</p>
      </div>
    </section>
  );
}

function CommandLabel({ children }: { children: ReactNode }) {
  return (
    <p className="mb-2 text-caption font-medium uppercase tracking-[0.08em] text-[#0a0d12]/55">
      {children}
    </p>
  );
}

function CommandRow({
  cmd,
  copyLabel,
  copiedLabel,
}: {
  cmd: string;
  copyLabel: string;
  copiedLabel: string;
}) {
  const [copied, setCopied] = useState(false);

  const onCopy = async () => {
    if (await copyText(cmd)) {
      setCopied(true);
      setTimeout(() => setCopied(false), 1800);
    }
  };

  return (
    <div className="flex items-start gap-3 rounded-xl border border-[#0a0d12]/10 bg-white px-4 py-3 font-mono text-label">
      <Terminal
        className="mt-0.5 size-4 shrink-0 text-[#0a0d12]/55"
        aria-hidden
      />
      <code className="min-w-0 flex-1 whitespace-pre-wrap break-all">
        {cmd}
      </code>
      <button
        type="button"
        onClick={onCopy}
        aria-label={copied ? copiedLabel : copyLabel}
        className="inline-flex shrink-0 items-center gap-1.5 rounded-md px-2 py-1 text-caption font-medium text-[#0a0d12]/70 transition-colors hover:bg-[#0a0d12]/5 hover:text-[#0a0d12]"
      >
        {copied ? (
          <>
            <Check className="size-3.5" />
            {copiedLabel}
          </>
        ) : (
          <>
            <Copy className="size-3.5" />
            {copyLabel}
          </>
        )}
      </button>
    </div>
  );
}
