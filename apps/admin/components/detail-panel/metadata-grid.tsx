import type { WorkspaceMetadata } from "@/lib/types";

// metadata.gitRemote comes from workspace.repos (JSONB set by whatever wrote
// the workspace row) and is rendered as a clickable link — only ever treat it
// as a link for http(s) URLs. Anything else (a bare "git@host:repo.git" SSH
// remote, or a malicious "javascript:..." value) renders as plain text
// instead of becoming an executable/unexpected href.
function safeHttpUrl(value: string): string | null {
  try {
    const url = new URL(value);
    return url.protocol === "http:" || url.protocol === "https:" ? value : null;
  } catch {
    return null;
  }
}

function formatDate(iso: string): string {
  return new Date(iso).toLocaleDateString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
  });
}

function Field({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div>
      <dt className="text-label text-muted-foreground">{label}</dt>
      <dd className="mt-0.5 text-body text-foreground break-all">{value}</dd>
    </div>
  );
}

function GitRemoteValue({ value }: { value: string | null }) {
  if (!value) return <span className="text-muted-foreground">Not reported</span>;
  const href = safeHttpUrl(value);
  if (!href) return value;
  return (
    <a href={href} className="text-primary hover:underline" target="_blank" rel="noreferrer">
      {value}
    </a>
  );
}

export function MetadataGrid({ metadata }: { metadata: WorkspaceMetadata }) {
  return (
    <section>
      <h3 className="mb-3 text-label font-medium text-muted-foreground uppercase tracking-wide">
        Metadata
      </h3>
      <dl className="grid grid-cols-2 gap-4">
        <Field label="Workspace ID" value={<span className="font-mono text-caption">{metadata.id}</span>} />
        <Field label="Owner" value={metadata.owner ?? <span className="text-muted-foreground">Unassigned</span>} />
        <Field label="Model" value={metadata.model ?? <span className="text-muted-foreground">Not set</span>} />
        <Field label="Created" value={formatDate(metadata.createdAt)} />
        <Field
          label="Root path"
          value={metadata.root ?? <span className="text-muted-foreground">Not reported</span>}
        />
        <Field label="Git remote" value={<GitRemoteValue value={metadata.gitRemote} />} />
      </dl>
    </section>
  );
}
