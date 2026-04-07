"use client";

import { useEffect, useState } from "react";
import { Save, Plus, Trash2, GitBranch, Check, ChevronsUpDown } from "lucide-react";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import {
  Popover,
  PopoverTrigger,
  PopoverContent,
} from "@/components/ui/popover";
import {
  Command,
  CommandInput,
  CommandList,
  CommandEmpty,
  CommandGroup,
  CommandItem,
} from "@/components/ui/command";
import { toast } from "sonner";
import { useAuthStore } from "@/features/auth";
import { useWorkspaceStore } from "@/features/workspace";
import { api } from "@/shared/api";
import type { WorkspaceRepo } from "@/shared/types";

const BRANCH_SUGGESTIONS = [
  { value: "", label: "Auto-detect", description: "Use remote default branch" },
  { value: "main", label: "main", description: "" },
  { value: "develop", label: "develop", description: "" },
  { value: "staging", label: "staging", description: "" },
  { value: "master", label: "master", description: "" },
];

function BranchPicker({
  value,
  onChange,
  disabled,
}: {
  value: string;
  onChange: (value: string) => void;
  disabled?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");

  const displayLabel = value || "Auto-detect";

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        render={
          <Button
            variant="outline"
            size="sm"
            className="h-8 w-full justify-between text-xs font-normal"
            disabled={disabled}
          >
            <span className="flex items-center gap-1.5 truncate">
              <GitBranch className="h-3 w-3 shrink-0 text-muted-foreground" />
              <span className={value ? "" : "text-muted-foreground"}>{displayLabel}</span>
            </span>
            <ChevronsUpDown className="h-3 w-3 shrink-0 text-muted-foreground" />
          </Button>
        }
      />
      <PopoverContent className="w-56 p-0" align="start">
        <Command>
          <CommandInput
            value={search}
            onValueChange={setSearch}
            placeholder="Branch name..."
            className="text-xs"
          />
          <CommandList>
            <CommandEmpty>
              {search.trim() ? (
                <button
                  type="button"
                  className="flex w-full items-center gap-2 px-2 py-1.5 text-sm hover:bg-accent rounded-sm transition-colors"
                  onClick={() => {
                    onChange(search.trim());
                    setOpen(false);
                    setSearch("");
                  }}
                >
                  <GitBranch className="h-3.5 w-3.5 text-muted-foreground" />
                  Use &ldquo;{search.trim()}&rdquo;
                </button>
              ) : (
                <span className="text-muted-foreground text-xs">Type a branch name</span>
              )}
            </CommandEmpty>
            <CommandGroup>
              {BRANCH_SUGGESTIONS.map((branch) => (
                <CommandItem
                  key={branch.value}
                  value={branch.label}
                  onSelect={() => {
                    onChange(branch.value);
                    setOpen(false);
                    setSearch("");
                  }}
                  className="text-xs"
                >
                  <div className="flex items-center gap-2 flex-1">
                    <GitBranch className="h-3 w-3 shrink-0 text-muted-foreground" />
                    <span>{branch.label}</span>
                    {branch.description && (
                      <span className="text-muted-foreground ml-auto text-[10px]">{branch.description}</span>
                    )}
                  </div>
                  {value === branch.value && <Check className="h-3 w-3 shrink-0" />}
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}

export function RepositoriesTab() {
  const user = useAuthStore((s) => s.user);
  const workspace = useWorkspaceStore((s) => s.workspace);
  const members = useWorkspaceStore((s) => s.members);
  const updateWorkspace = useWorkspaceStore((s) => s.updateWorkspace);

  const [repos, setRepos] = useState<WorkspaceRepo[]>(workspace?.repos ?? []);
  const [saving, setSaving] = useState(false);

  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const canManageWorkspace = currentMember?.role === "owner" || currentMember?.role === "admin";

  useEffect(() => {
    setRepos(workspace?.repos ?? []);
  }, [workspace]);

  const handleSave = async () => {
    if (!workspace) return;
    setSaving(true);
    try {
      const updated = await api.updateWorkspace(workspace.id, { repos });
      updateWorkspace(updated);
      toast.success("Repositories saved");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to save repositories");
    } finally {
      setSaving(false);
    }
  };

  const handleAddRepo = () => {
    setRepos([...repos, { url: "", description: "", default_branch: "" }]);
  };

  const handleRemoveRepo = (index: number) => {
    setRepos(repos.filter((_, i) => i !== index));
  };

  const handleRepoChange = (index: number, field: keyof WorkspaceRepo, value: string) => {
    setRepos(repos.map((r, i) => (i === index ? { ...r, [field]: value } : r)));
  };

  if (!workspace) return null;

  return (
    <div className="space-y-8">
      <section className="space-y-4">
        <h2 className="text-sm font-semibold">Repositories</h2>

        <Card>
          <CardContent className="space-y-3">
            <p className="text-xs text-muted-foreground">
              GitHub repositories associated with this workspace. Agents use these to clone and work on code.
            </p>

            {repos.map((repo, index) => (
              <div key={index} className="flex gap-2">
                <div className="flex-1 space-y-1.5">
                  <Input
                    type="url"
                    value={repo.url}
                    onChange={(e) => handleRepoChange(index, "url", e.target.value)}
                    disabled={!canManageWorkspace}
                    placeholder="https://github.com/org/repo"
                    className="text-sm"
                  />
                  <Input
                    type="text"
                    value={repo.description}
                    onChange={(e) => handleRepoChange(index, "description", e.target.value)}
                    disabled={!canManageWorkspace}
                    placeholder="Description (e.g. Go backend + Next.js frontend)"
                    className="text-sm"
                  />
                  <div className="flex items-center gap-2">
                    <span className="text-xs text-muted-foreground shrink-0">PR target branch</span>
                    <div className="w-48">
                      <BranchPicker
                        value={repo.default_branch ?? ""}
                        onChange={(v) => handleRepoChange(index, "default_branch", v)}
                        disabled={!canManageWorkspace}
                      />
                    </div>
                  </div>
                </div>
                {canManageWorkspace && (
                  <Button
                    variant="ghost"
                    size="icon"
                    className="mt-0.5 shrink-0 text-muted-foreground hover:text-destructive"
                    onClick={() => handleRemoveRepo(index)}
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </Button>
                )}
              </div>
            ))}

            {canManageWorkspace && (
              <div className="flex items-center justify-between pt-1">
                <Button variant="outline" size="sm" onClick={handleAddRepo}>
                  <Plus className="h-3 w-3" />
                  Add repository
                </Button>
                <Button
                  size="sm"
                  onClick={handleSave}
                  disabled={saving}
                >
                  <Save className="h-3 w-3" />
                  {saving ? "Saving..." : "Save"}
                </Button>
              </div>
            )}

            {!canManageWorkspace && (
              <p className="text-xs text-muted-foreground">
                Only admins and owners can manage repositories.
              </p>
            )}
          </CardContent>
        </Card>
      </section>
    </div>
  );
}
