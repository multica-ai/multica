"use client";

import { useState } from "react";
import { ChevronRight, ExternalLink, Link2, Plus, X } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { workspaceIntegrationsOptions, issueIntegrationLinksOptions, projectIntegrationLinksOptions, redmineIssuesOptions } from "@multica/core/integrations/queries";
import { useUpsertIssueIntegrationLink, useDeleteIssueIntegrationLink } from "@multica/core/integrations/mutations";
import { api } from "@multica/core/api";
import { Popover, PopoverTrigger, PopoverContent } from "@multica/ui/components/ui/popover";
import { toast } from "sonner";
import type { RedmineIssue } from "@multica/core/types";

// ---------------------------------------------------------------------------
// PropRow (mirrors the one in issue-detail, avoids coupling)
// ---------------------------------------------------------------------------

function PropRow({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex min-h-8 items-center gap-2 rounded-md px-2 -mx-2 hover:bg-accent/50 transition-colors">
      <span className="w-16 shrink-0 text-xs text-muted-foreground">{label}</span>
      <div className="flex min-w-0 flex-1 items-center gap-1.5 text-xs truncate">
        {children}
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Link/create popover
// ---------------------------------------------------------------------------

function LinkOrCreatePopover({
  wsId,
  issueId,
  issueTitle,
  issueDescription,
  redmineProjectId,
  onClose,
}: {
  wsId: string;
  issueId: string;
  issueTitle: string;
  issueDescription?: string | null;
  redmineProjectId: number | null;
  onClose: () => void;
}) {
  const [filter, setFilter] = useState("");
  const [creating, setCreating] = useState(false);
  const upsert = useUpsertIssueIntegrationLink();

  const { data: issuesData, isLoading } = useQuery({
    ...redmineIssuesOptions(wsId, redmineProjectId ?? 0),
    enabled: !!redmineProjectId,
  });
  const redmineIssues: RedmineIssue[] = issuesData?.issues ?? [];

  const filtered = filter.trim()
    ? redmineIssues.filter(
      (i) =>
        i.subject.toLowerCase().includes(filter.toLowerCase()) ||
        String(i.id).includes(filter),
    )
    : redmineIssues;

  const handleLink = async (issue: RedmineIssue) => {
    try {
      await upsert.mutateAsync({
        issueId,
        provider: "redmine",
        external_issue_id: String(issue.id),
        external_issue_title: issue.subject,
        external_issue_url: null,
      });
      toast.success("Linked to Redmine issue");
      onClose();
    } catch {
      toast.error("Failed to link issue");
    }
  };

  const handleCreate = async () => {
    if (!redmineProjectId) {
      toast.error("Link this project to Redmine first");
      return;
    }
    const multicaUrl = typeof window !== "undefined" ? window.location.href : "";
    const descParts: string[] = [];
    if (issueDescription) descParts.push(issueDescription);
    descParts.push("---");
    descParts.push(`Creado desde Multica${multicaUrl ? ` · ${multicaUrl}` : ""}`);
    const description = descParts.join("\n");

    setCreating(true);
    try {
      const created = await api.createRedmineIssue({
        project_id: redmineProjectId,
        subject: issueTitle,
        description,
      });
      await upsert.mutateAsync({
        issueId,
        provider: "redmine",
        external_issue_id: String(created.id),
        external_issue_title: created.subject,
        external_issue_url: null,
      });
      toast.success("Issue created and linked in Redmine");
      onClose();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to create issue in Redmine");
    } finally {
      setCreating(false);
    }
  };

  return (
    <div className="w-64">
      <div className="px-2 py-1.5 border-b">
        <input
          type="text"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          placeholder={redmineProjectId ? "Search issues..." : "Search..."}
          className="w-full bg-transparent text-sm placeholder:text-muted-foreground outline-none"
          autoFocus
        />
      </div>
      <div className="max-h-60 overflow-y-auto p-1">
        {!redmineProjectId && (
          <p className="px-2 py-3 text-xs text-muted-foreground text-center">
            Link this project to Redmine first to browse issues.
          </p>
        )}
        {redmineProjectId && isLoading && (
          <p className="px-2 py-3 text-xs text-muted-foreground text-center">Loading...</p>
        )}
        {filtered.map((issue) => (
          <button
            key={issue.id}
            type="button"
            onClick={() => handleLink(issue)}
            className="flex w-full items-start gap-2 rounded-md px-2 py-1.5 text-left text-sm hover:bg-accent transition-colors"
          >
            <span className="shrink-0 text-xs text-muted-foreground tabular-nums pt-0.5">
              #{issue.id}
            </span>
            <span className="min-w-0 truncate">{issue.subject}</span>
          </button>
        ))}
        {filtered.length === 0 && redmineProjectId && !isLoading && (
          <p className="px-2 py-2 text-xs text-muted-foreground text-center">No issues found</p>
        )}
      </div>
      <div className="border-t p-1">
        <button
          type="button"
          onClick={handleCreate}
          disabled={creating || upsert.isPending}
          className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-accent transition-colors disabled:opacity-50"
        >
          <Plus className="h-3.5 w-3.5 text-muted-foreground shrink-0" />
          <span>{creating ? "Creating..." : "Create new in Redmine"}</span>
        </button>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// IssueRedmineSection
// ---------------------------------------------------------------------------

interface IssueRedmineSectionProps {
  wsId: string;
  issueId: string;
  issueTitle: string;
  issueDescription?: string | null;
  projectId: string | null;
}

export function IssueRedmineSection({ wsId, issueId, issueTitle, issueDescription, projectId }: IssueRedmineSectionProps) {
  const [open, setOpen] = useState(false);
  const [popoverOpen, setPopoverOpen] = useState(false);

  const { data: integrationsData } = useQuery(workspaceIntegrationsOptions(wsId));
  const { data: linksData } = useQuery(issueIntegrationLinksOptions(wsId, issueId));
  const { data: projectLinksData } = useQuery({
    ...projectIntegrationLinksOptions(wsId, projectId ?? ""),
    enabled: !!projectId,
  });

  const deleteLink = useDeleteIssueIntegrationLink();

  const redmineIntegration = (integrationsData?.integrations ?? []).find(
    (i) => i.provider === "redmine",
  );
  if (!redmineIntegration) return null;

  const redmineLink = (linksData?.links ?? []).find((l) => l.provider === "redmine");
  const projectRedmineLink = (projectLinksData?.links ?? []).find((l) => l.provider === "redmine");
  const redmineProjectId = projectRedmineLink
    ? parseInt(projectRedmineLink.external_project_id, 10)
    : null;

  const issueURL = redmineLink?.external_issue_url ??
    (redmineLink
      ? `${redmineIntegration.instance_url.replace(/\/$/, "")}/issues/${redmineLink.external_issue_id}`
      : null);

  const handleUnlink = async () => {
    try {
      await deleteLink.mutateAsync({ issueId, provider: "redmine" });
      toast.success("Unlinked from Redmine");
    } catch {
      toast.error("Failed to unlink");
    }
  };

  return (
    <div>
      <button
        className={`flex w-full items-center gap-1 rounded-md px-2 py-1 text-xs font-medium transition-colors mb-2 hover:bg-accent/70 ${open ? "" : "text-muted-foreground hover:text-foreground"}`}
        onClick={() => setOpen(!open)}
      >
        Redmine
        <ChevronRight
          className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${open ? "rotate-90" : ""}`}
        />
      </button>

      {open && (
        <div className="space-y-0.5 pl-2">
          {redmineLink ? (
            <PropRow label="Issue">
              <a
                href={issueURL ?? "#"}
                target="_blank"
                rel="noopener noreferrer"
                className="flex items-center gap-1 min-w-0 hover:text-foreground"
              >
                <ExternalLink className="h-3 w-3 shrink-0" />
                <span className="truncate">
                  #{redmineLink.external_issue_id}
                  {redmineLink.external_issue_title
                    ? ` · ${redmineLink.external_issue_title}`
                    : ""}
                </span>
              </a>
              <button
                type="button"
                onClick={handleUnlink}
                disabled={deleteLink.isPending}
                className="ml-auto shrink-0 text-muted-foreground hover:text-destructive"
                title="Unlink"
              >
                <X className="h-3 w-3" />
              </button>
            </PropRow>
          ) : (
            <Popover open={popoverOpen} onOpenChange={setPopoverOpen}>
              <PopoverTrigger
                render={
                  <button
                    type="button"
                    className="flex w-full items-center gap-1.5 rounded-md px-2 py-1.5 text-xs text-muted-foreground hover:bg-accent hover:text-foreground transition-colors"
                  >
                    <Link2 className="h-3 w-3 shrink-0" />
                    Link to Redmine issue
                  </button>
                }
              />
              <PopoverContent align="start" className="w-auto p-0">
                <LinkOrCreatePopover
                  wsId={wsId}
                  issueId={issueId}
                  issueTitle={issueTitle}
                  issueDescription={issueDescription}
                  redmineProjectId={redmineProjectId}
                  onClose={() => setPopoverOpen(false)}
                />
              </PopoverContent>
            </Popover>
          )}
        </div>
      )}
    </div>
  );
}
