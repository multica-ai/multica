"use client";

// CEREBRO-PATCH(runtime-setup-page): cerebro modification of upstream file

import { ArrowLeft, Server, Terminal, Apple } from "lucide-react";
import { useNavigation } from "../navigation";
import { useWorkspaceId } from "@multica/core/hooks";
import { useQuery } from "@tanstack/react-query";
import { workspaceListOptions } from "@multica/core/workspace/queries";

export function RuntimeSetupDocsPage() {
  const wsId = useWorkspaceId();
  const { push } = useNavigation();
  const { data: workspaces = [] } = useQuery(workspaceListOptions());
  const slug = workspaces.find((w) => w.id === wsId)?.slug ?? "";

  return (
    <div className="mx-auto w-full max-w-3xl px-4 sm:px-6 py-6 sm:py-10">
      <button
        onClick={() => push(`/${slug}/runtimes`)}
        className="mb-6 inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
      >
        <ArrowLeft className="h-3.5 w-3.5" />
        Back to runtimes
      </button>

      <header className="mb-8">
        <div className="mb-3 inline-flex h-10 w-10 items-center justify-center rounded-md bg-muted">
          <Server className="h-5 w-5" />
        </div>
        <h1 className="text-2xl font-semibold tracking-tight">Set up a runtime</h1>
        <p className="mt-2 text-sm text-muted-foreground">
          A runtime is a daemon that runs on your laptop or a shared box and
          executes agent tasks (Claude, Codex) on your behalf. Each user
          installs their own runtime — Multica routes assigned tasks to the
          runtime its owner has online.
        </p>
      </header>

      <Section icon={<Terminal className="h-4 w-4" />} title="Recommended: one-line installer">
        <p>
          On the Runtimes page, click <strong>Add runtime</strong>. You'll get a
          one-line install command like:
        </p>
        <Code>
          curl -fsSL https://your-server.example/install-runtime.sh | bash -s -- --token mst_…
        </Code>
        <p>
          Run it on the new machine. The script:
        </p>
        <ol className="ml-5 list-decimal space-y-1">
          <li>Installs the <code>multica</code> CLI (Homebrew on macOS, curl-based on Linux)</li>
          <li>Exchanges the setup token for a per-user daemon token</li>
          <li>Writes config to <code>~/.multica/config.json</code></li>
          <li>Installs a launchd autostart job (macOS) and starts the daemon</li>
        </ol>
        <p className="text-xs text-muted-foreground">
          Setup tokens are single-use and expire after 30 minutes. Generate a
          new one if it expires before you've run it.
        </p>
      </Section>

      <Section icon={<Apple className="h-4 w-4" />} title="Prerequisites">
        <ul className="ml-5 list-disc space-y-1">
          <li>
            <strong>macOS</strong> with Homebrew, or <strong>Linux</strong>
          </li>
          <li>
            Network access to your Multica server (e.g. via Tailscale on the
            same tailnet as the host)
          </li>
          <li>
            An agent CLI installed and logged in:
            <ul className="ml-5 mt-1 list-disc space-y-0.5">
              <li>
                Claude: <code>claude</code> CLI installed, run <code>claude login</code>
              </li>
              <li>
                Codex: <code>codex</code> CLI installed, run <code>codex login</code>
              </li>
            </ul>
          </li>
        </ul>
      </Section>

      <Section title="Manual setup (advanced)">
        <p>
          If you can't run the one-line installer (e.g. on a locked-down host),
          do the steps manually:
        </p>
        <Code>
          {`brew install multica-ai/tap/multica
multica config set server-url <your-server>
multica auth login
multica daemon start --profile local`}
        </Code>
        <p>
          The <code>auth login</code> step opens a browser to your Multica
          server for OAuth — same flow as logging into the web app.
        </p>
      </Section>

      <Section title="Verify it's online">
        <ol className="ml-5 list-decimal space-y-1">
          <li>
            Run <code>multica daemon status</code> on the new machine — it
            should report <strong>running</strong>.
          </li>
          <li>
            Reload the Runtimes page in Multica. The new runtime should appear
            with a green "online" dot.
          </li>
          <li>
            Assign an issue to an agent backed by this runtime to test
            end-to-end.
          </li>
        </ol>
      </Section>

      <Section title="Troubleshooting">
        <dl className="space-y-3">
          <Item term="Daemon doesn't appear in the list">
            Check <code>~/.multica/daemon.err.log</code> on the host. Common
            causes: server URL unreachable from this machine, expired daemon
            token, or no Tailscale connection.
          </Item>
          <Item term="Agent CLI not found">
            The daemon delegates work to <code>claude</code> or <code>codex</code>.
            Ensure these are on the daemon's <code>PATH</code> — for launchd
            jobs that means installing them globally (e.g. via Homebrew) and
            re-running the autostart setup so the plist picks up the path.
          </Item>
          <Item term="Tasks stuck pending">
            Visit the runtime's detail panel and click <strong>Ping</strong> —
            this round-trips a no-op task and surfaces any handler errors.
          </Item>
          <Item term="Need to revoke a runtime's token">
            From the runtime detail page, delete the runtime — that revokes the
            daemon token. The user can re-onboard with a fresh setup token.
          </Item>
        </dl>
      </Section>
    </div>
  );
}

function Section({
  icon,
  title,
  children,
}: {
  icon?: React.ReactNode;
  title: string;
  children: React.ReactNode;
}) {
  return (
    <section className="mb-10">
      <h2 className="mb-3 flex items-center gap-2 text-base font-semibold">
        {icon}
        {title}
      </h2>
      <div className="space-y-3 text-sm leading-relaxed text-muted-foreground [&_code]:rounded [&_code]:bg-muted [&_code]:px-1 [&_code]:py-0.5 [&_code]:text-xs [&_code]:text-foreground [&_strong]:text-foreground">
        {children}
      </div>
    </section>
  );
}

function Code({ children }: { children: React.ReactNode }) {
  return (
    <pre className="my-2 overflow-x-auto rounded-md border bg-muted/40 p-3 text-xs leading-relaxed text-foreground">
      <code>{children}</code>
    </pre>
  );
}

function Item({ term, children }: { term: string; children: React.ReactNode }) {
  return (
    <div>
      <dt className="text-foreground">{term}</dt>
      <dd className="mt-1">{children}</dd>
    </div>
  );
}
