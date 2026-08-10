"use client";

import { useState } from "react";
import { Check, Copy, Terminal } from "lucide-react";
import { copyText } from "@multica/ui/lib/clipboard";
import { useLocale } from "../../i18n";

type CliPlatform = "unix" | "windows";

const INSTALL_COMMANDS: Record<CliPlatform, string> = {
  unix: "curl -fsSL https://raw.githubusercontent.com/SeimoDev/multica/main/scripts/install.sh | bash",
  windows:
    "irm https://raw.githubusercontent.com/SeimoDev/multica/main/scripts/install.ps1 | iex",
};
const SETUP_CMD = "multica setup";

/**
 * Scenario-first CLI section. Copy leans into servers / remote dev
 * boxes / headless setups rather than positioning CLI as a
 * lightweight Desktop. Two copy-and-paste command blocks.
 */
export function CliSection() {
  const { t } = useLocale();
  const d = t.download.cli;
  const [platform, setPlatform] = useState<CliPlatform>("unix");

  return (
    <section id="cli" className="bg-[#f7f7f5] py-20 text-[#0a0d12] sm:py-24">
      <div className="mx-auto max-w-[820px] px-4 sm:px-6 lg:px-8">
        <h2 className="landing-serif text-[2.2rem] leading-[1.1] tracking-[-0.03em] sm:text-[2.6rem]">
          {d.title}
        </h2>
        <p className="mt-4 max-w-[620px] text-body-lg leading-7 text-[#0a0d12]/72">
          {d.sub}
        </p>

        <div
          role="group"
          aria-label={d.platformLabel}
          className="mt-8 inline-flex rounded-lg bg-[#0a0d12]/6 p-1"
        >
          {(
            [
              ["unix", d.platformUnix],
              ["windows", d.platformWindows],
            ] as const
          ).map(([value, label]) => (
            <button
              key={value}
              type="button"
              aria-pressed={platform === value}
              onClick={() => setPlatform(value)}
              className={`rounded-md px-4 py-2 text-label font-medium transition-colors ${
                platform === value
                  ? "bg-white text-[#0a0d12] shadow-sm"
                  : "text-[#0a0d12]/60 hover:text-[#0a0d12]"
              }`}
            >
              {label}
            </button>
          ))}
        </div>

        <div className="mt-6 flex flex-col gap-5">
          <CommandBlock
            label={d.installLabel}
            cmd={INSTALL_COMMANDS[platform]}
            copyLabel={d.copyLabel}
            copiedLabel={d.copiedLabel}
          />
          <CommandBlock
            label={d.startLabel}
            cmd={SETUP_CMD}
            copyLabel={d.copyLabel}
            copiedLabel={d.copiedLabel}
          />
        </div>

        <p className="mt-6 text-label text-[#0a0d12]/60">{d.sshNote}</p>
      </div>
    </section>
  );
}

function CommandBlock({
  label,
  cmd,
  copyLabel,
  copiedLabel,
}: {
  label: string;
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
    <div>
      <p className="mb-2 text-caption font-medium uppercase tracking-[0.08em] text-[#0a0d12]/55">
        {label}
      </p>
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
    </div>
  );
}
