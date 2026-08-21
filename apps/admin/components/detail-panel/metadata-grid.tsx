import type { WorkspaceMetadata } from "@/lib/types";

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

function RepoCountValue({ count }: { count: number }) {
  if (count === 0) return <span className="text-muted-foreground">No repos connected</span>;
  return `${count} repo${count === 1 ? "" : "s"} connected`;
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
        <Field label="Repos" value={<RepoCountValue count={metadata.repoCount} />} />
      </dl>
    </section>
  );
}
