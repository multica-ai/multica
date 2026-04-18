"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  Check,
  Clock,
  Copy,
  Link2,
  Loader2,
  MessageSquare,
  Plus,
  SearchIcon,
  Inbox,
  CircleUser,
  ListTodo,
  FolderKanban,
  Bot,
  Monitor,
  Moon,
  Sun,
  BookOpenText,
  Settings,
  Building2,
  type LucideIcon,
} from "lucide-react";
import { Command as CommandPrimitive } from "cmdk";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import type { SearchIssueResult, SearchProjectResult } from "@multica/core/types";
import { api } from "@multica/core/api";
import { useRecentIssuesStore } from "@multica/core/issues/stores";
import { issueListOptions } from "@multica/core/issues/queries";
import { useWorkspaceId } from "@multica/core";
import { paths, useCurrentWorkspace, useWorkspacePaths } from "@multica/core/paths";
import type { WorkspacePaths } from "@multica/core/paths";
import { useModalStore } from "@multica/core/modals";
import { workspaceListOptions } from "@multica/core/workspace/queries";
import { StatusIcon } from "../issues/components";
import { STATUS_CONFIG } from "@multica/core/issues/config";
import { PROJECT_STATUS_CONFIG } from "@multica/core/projects/config";
import type { ProjectStatus } from "@multica/core/types";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@multica/ui/components/ui/dialog";
import { useTheme } from "@multica/ui/components/common/theme-provider";
import { useNavigation } from "../navigation";
import { useSearchStore } from "./search-store";

function HighlightText({ text, query }: { text: string; query: string }) {
  const parts = useMemo(() => {
    if (!query.trim()) return [{ text, highlight: false }];
    const escaped = query.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    const regex = new RegExp(`(${escaped})`, "gi");
    const result: { text: string; highlight: boolean }[] = [];
    let lastIndex = 0;
    let match: RegExpExecArray | null;
    while ((match = regex.exec(text)) !== null) {
      if (match.index > lastIndex) {
        result.push({ text: text.slice(lastIndex, match.index), highlight: false });
      }
      result.push({ text: match[0], highlight: true });
      lastIndex = regex.lastIndex;
    }
    if (lastIndex < text.length) {
      result.push({ text: text.slice(lastIndex), highlight: false });
    }
    return result.length > 0 ? result : [{ text, highlight: false }];
  }, [text, query]);

  return (
    <>
      {parts.map((part, i) =>
        part.highlight ? (
          <mark key={i} className="bg-yellow-200 dark:bg-yellow-900/60 text-inherit rounded-sm">
            {part.text}
          </mark>
        ) : (
          part.text
        ),
      )}
    </>
  );
}

// Nav items reference WorkspacePaths method names so they can be resolved
// against the current workspace slug at render time (see SearchCommand body).
// Only parameterless paths are valid nav destinations.
type NavKey =
  | "inbox"
  | "myIssues"
  | "issues"
  | "projects"
  | "agents"
  | "runtimes"
  | "skills"
  | "settings";

interface NavPage {
  key: NavKey;
  label: string;
  icon: LucideIcon;
  keywords: string[];
}

const navPages: NavPage[] = [
  { key: "inbox", label: "Inbox", icon: Inbox, keywords: ["inbox", "notifications"] },
  { key: "myIssues", label: "My Issues", icon: CircleUser, keywords: ["my", "issues", "assigned"] },
  { key: "issues", label: "Issues", icon: ListTodo, keywords: ["issues", "tasks", "bugs"] },
  { key: "projects", label: "Projects", icon: FolderKanban, keywords: ["projects", "kanban"] },
  { key: "agents", label: "Agents", icon: Bot, keywords: ["agents", "bots", "ai"] },
  { key: "runtimes", label: "Runtimes", icon: Monitor, keywords: ["runtimes", "environments"] },
  { key: "skills", label: "Skills", icon: BookOpenText, keywords: ["skills", "library"] },
  { key: "settings", label: "Settings", icon: Settings, keywords: ["settings", "config", "preferences"] },
];

type ThemeValue = "light" | "dark" | "system";

interface CommandItem {
  key: string;
  label: string;
  icon: LucideIcon;
  keywords: string[];
  trailing?: React.ReactNode;
  onSelect: () => void;
}

interface SearchResults {
  issues: SearchIssueResult[];
  projects: SearchProjectResult[];
}

export function SearchCommand() {
  const { push, pathname, getShareableUrl } = useNavigation();
  const open = useSearchStore((s) => s.open);
  const setOpen = useSearchStore((s) => s.setOpen);
  const recentItems = useRecentIssuesStore((s) => s.items);
  const wsId = useWorkspaceId();
  const p: WorkspacePaths = useWorkspacePaths();
  const { data: allIssues = [] } = useQuery(issueListOptions(wsId));
  const { theme, setTheme } = useTheme();
  const currentWorkspace = useCurrentWorkspace();
  const { data: workspaces = [] } = useQuery(workspaceListOptions());

  const recentIssues = useMemo(() => {
    const issueMap = new Map(allIssues.map((i) => [i.id, i]));
    return recentItems.flatMap((item) => {
      const issue = issueMap.get(item.id);
      return issue ? [issue] : [];
    });
  }, [recentItems, allIssues]);

  const [query, setQuery] = useState("");
  const [results, setResults] = useState<SearchResults>({ issues: [], projects: [] });
  const [isLoading, setIsLoading] = useState(false);
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const abortRef = useRef<AbortController | null>(null);

  const filteredPages = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return [];
    return navPages.filter(
      (page) =>
        page.label.toLowerCase().includes(q) ||
        page.keywords.some((kw) => kw.includes(q)),
    );
  }, [query]);

  // Detect if current route is an issue detail page — /{slug}/issues/{id}.
  // Falls back to null on any other route; used to gate issue-specific commands.
  const currentIssue = useMemo(() => {
    const match = pathname.match(/\/issues\/([^/]+)$/);
    const raw = match?.[1];
    if (!raw) return null;
    const id = decodeURIComponent(raw);
    return allIssues.find((i) => i.id === id) ?? null;
  }, [pathname, allIssues]);

  const commands = useMemo<CommandItem[]>(() => {
    const activeThemeCheck = (value: ThemeValue) =>
      theme === value ? (
        <Check
          aria-label="Current theme"
          className="ml-auto size-4 shrink-0 text-muted-foreground"
        />
      ) : undefined;

    const items: CommandItem[] = [
      {
        key: "new-issue",
        label: "New Issue",
        icon: Plus,
        keywords: ["new", "issue", "create", "add"],
        onSelect: () => {
          useModalStore.getState().open("create-issue");
          setOpen(false);
        },
      },
      {
        key: "new-project",
        label: "New Project",
        icon: Plus,
        keywords: ["new", "project", "create", "add"],
        onSelect: () => {
          useModalStore.getState().open("create-project");
          setOpen(false);
        },
      },
    ];

    if (currentIssue) {
      const identifier = currentIssue.identifier;
      items.push(
        {
          key: "copy-issue-link",
          label: "Copy Issue Link",
          icon: Link2,
          keywords: ["copy", "link", "share", "url", identifier.toLowerCase()],
          onSelect: () => {
            const url = getShareableUrl ? getShareableUrl(pathname) : window.location.href;
            void navigator.clipboard.writeText(url);
            toast.success("Link copied");
            setOpen(false);
          },
        },
        {
          key: "copy-issue-identifier",
          label: `Copy Identifier (${identifier})`,
          icon: Copy,
          keywords: ["copy", "id", "identifier", identifier.toLowerCase()],
          onSelect: () => {
            void navigator.clipboard.writeText(identifier);
            toast.success(`Copied ${identifier}`);
            setOpen(false);
          },
        },
      );
    }

    items.push(
      {
        key: "theme-light",
        label: "Switch to Light Theme",
        icon: Sun,
        keywords: ["light", "theme", "appearance", "mode", "bright"],
        trailing: activeThemeCheck("light"),
        onSelect: () => {
          setTheme("light");
          setOpen(false);
        },
      },
      {
        key: "theme-dark",
        label: "Switch to Dark Theme",
        icon: Moon,
        keywords: ["dark", "theme", "appearance", "mode", "night"],
        trailing: activeThemeCheck("dark"),
        onSelect: () => {
          setTheme("dark");
          setOpen(false);
        },
      },
      {
        key: "theme-system",
        label: "Use System Theme",
        icon: Monitor,
        keywords: ["system", "theme", "appearance", "mode", "auto"],
        trailing: activeThemeCheck("system"),
        onSelect: () => {
          setTheme("system");
          setOpen(false);
        },
      },
    );

    return items;
  }, [currentIssue, getShareableUrl, pathname, setOpen, setTheme, theme]);

  const filteredCommands = useMemo(() => {
    const q = query.trim().toLowerCase();
    // No query: only surface the primary creation action. Other commands
    // (theme switches, copy actions, New Project) are revealed as the user
    // types, leaving the empty-state space to Recent.
    if (!q) return commands.filter((c) => c.key === "new-issue");
    return commands.filter(
      (c) =>
        c.label.toLowerCase().includes(q) ||
        c.keywords.some((kw) => kw.includes(q)),
    );
  }, [commands, query]);

  // Only show workspaces different from the current one, and only after the
  // user types >=2 chars — one char would match everything (e.g. "w").
  const filteredWorkspaces = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return [];
    const others = workspaces.filter((w) => w.id !== currentWorkspace?.id);
    const wantsAll =
      q.length >= 2 && ("workspace".startsWith(q) || "switch".startsWith(q));
    return others.filter(
      (w) =>
        wantsAll ||
        w.name.toLowerCase().includes(q) ||
        w.slug.toLowerCase().includes(q),
    );
  }, [workspaces, currentWorkspace?.id, query]);

  const hasResults = results.issues.length > 0 || results.projects.length > 0;

  // Global Cmd+K / Ctrl+K shortcut
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "k" && (e.metaKey || e.ctrlKey)) {
        e.preventDefault();
        useSearchStore.getState().toggle();
      }
    };
    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, []);

  // Close on single ESC — capture phase fires before base-ui Dialog's handlers
  useEffect(() => {
    if (!open) return;
    const handleEsc = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault();
        e.stopPropagation();
        setOpen(false);
      }
    };
    document.addEventListener("keydown", handleEsc, true);
    return () => document.removeEventListener("keydown", handleEsc, true);
  }, [open, setOpen]);

  // Cleanup debounce/abort on unmount
  useEffect(() => {
    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current);
      if (abortRef.current) abortRef.current.abort();
    };
  }, []);

  // Reset state when dialog closes
  useEffect(() => {
    if (!open) {
      setQuery("");
      setResults({ issues: [], projects: [] });
      setIsLoading(false);
    }
  }, [open]);

  const search = useCallback((q: string) => {
    if (debounceRef.current) clearTimeout(debounceRef.current);
    if (abortRef.current) abortRef.current.abort();

    if (!q.trim()) {
      setResults({ issues: [], projects: [] });
      setIsLoading(false);
      return;
    }

    setIsLoading(true);
    debounceRef.current = setTimeout(async () => {
      const controller = new AbortController();
      abortRef.current = controller;
      try {
        const [issueRes, projectRes] = await Promise.all([
          api.searchIssues({
            q: q.trim(),
            limit: 20,
            include_closed: true,
            signal: controller.signal,
          }),
          api.searchProjects({
            q: q.trim(),
            limit: 10,
            include_closed: true,
            signal: controller.signal,
          }),
        ]);
        if (!controller.signal.aborted) {
          setResults({
            issues: issueRes.issues,
            projects: projectRes.projects,
          });
          setIsLoading(false);
        }
      } catch {
        if (!controller.signal.aborted) {
          setIsLoading(false);
        }
      }
    }, 300);
  }, []);

  const handleValueChange = useCallback(
    (value: string) => {
      setQuery(value);
      search(value);
    },
    [search],
  );

  const handleSelect = useCallback(
    (value: string) => {
      setOpen(false);
      if (value.startsWith("project:")) {
        // value is "project:<id>" — slice off the 8-char prefix to extract the id.
        push(p.projectDetail(value.slice(8)));
      } else {
        push(p.issueDetail(value));
      }
    },
    [push, setOpen, p],
  );

  const handlePageSelect = useCallback(
    (key: NavKey) => {
      setOpen(false);
      push(p[key]());
    },
    [push, setOpen, p],
  );

  const handleSwitchWorkspace = useCallback(
    (slug: string) => {
      push(paths.workspace(slug).issues());
      setOpen(false);
    },
    [push, setOpen],
  );

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogContent
        finalFocus={false}
        className="top-[20%] translate-y-0 overflow-hidden rounded-xl! p-0 sm:max-w-xl!"
        showCloseButton={false}
      >
        <DialogHeader className="sr-only">
          <DialogTitle>Search</DialogTitle>
          <DialogDescription>
            Search pages, issues, and projects
          </DialogDescription>
        </DialogHeader>
        <CommandPrimitive
          shouldFilter={false}
          className="flex size-full flex-col overflow-hidden rounded-xl bg-popover text-popover-foreground"
        >
          {/* Search input */}
          <div className="flex items-center gap-3 border-b px-4 py-3">
            <SearchIcon className="size-5 shrink-0 text-muted-foreground" />
            <CommandPrimitive.Input
              placeholder="Type a command or search..."
              value={query}
              onValueChange={handleValueChange}
              className="flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground"
            />
            <kbd className="hidden shrink-0 rounded bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground sm:inline">
              ESC
            </kbd>
          </div>

          {/* Results list */}
          <CommandPrimitive.List className="max-h-[min(400px,50vh)] overflow-y-auto overflow-x-hidden">
            {/* Pages section — only shown when query matches */}
            {filteredPages.length > 0 && (
              <CommandPrimitive.Group className="p-2">
                <div className="px-3 py-1.5 text-xs font-medium text-muted-foreground">
                  Pages
                </div>
                {filteredPages.map((page) => (
                  <CommandPrimitive.Item
                    key={page.key}
                    value={`page:${page.key}`}
                    onSelect={() => handlePageSelect(page.key)}
                    className="flex cursor-default select-none items-center gap-2.5 rounded-lg px-3 py-2.5 text-sm outline-none data-[disabled=true]:pointer-events-none data-[disabled=true]:opacity-50 data-selected:bg-accent"
                  >
                    <page.icon className="size-4 shrink-0 text-muted-foreground" />
                    <span className="truncate">
                      <HighlightText text={page.label} query={query} />
                    </span>
                  </CommandPrimitive.Item>
                ))}
              </CommandPrimitive.Group>
            )}

            {/* Commands section — New Issue / New Project / Copy link / Theme, only shown when query matches */}
            {filteredCommands.length > 0 && (
              <CommandPrimitive.Group className="p-2">
                <div className="px-3 py-1.5 text-xs font-medium text-muted-foreground">
                  Commands
                </div>
                {filteredCommands.map((cmd) => (
                  <CommandPrimitive.Item
                    key={cmd.key}
                    value={`command:${cmd.key}`}
                    onSelect={cmd.onSelect}
                    className="flex cursor-default select-none items-center gap-2.5 rounded-lg px-3 py-2.5 text-sm outline-none data-[disabled=true]:pointer-events-none data-[disabled=true]:opacity-50 data-selected:bg-accent"
                  >
                    <cmd.icon className="size-4 shrink-0 text-muted-foreground" />
                    <span className="truncate">
                      <HighlightText text={cmd.label} query={query} />
                    </span>
                    {cmd.trailing}
                  </CommandPrimitive.Item>
                ))}
              </CommandPrimitive.Group>
            )}

            {/* Workspaces section — switch to a different workspace, only shown when query matches */}
            {filteredWorkspaces.length > 0 && (
              <CommandPrimitive.Group className="p-2">
                <div className="px-3 py-1.5 text-xs font-medium text-muted-foreground">
                  Switch Workspace
                </div>
                {filteredWorkspaces.map((ws) => (
                  <CommandPrimitive.Item
                    key={ws.id}
                    value={`workspace:${ws.id}`}
                    onSelect={() => handleSwitchWorkspace(ws.slug)}
                    className="flex cursor-default select-none items-center gap-2.5 rounded-lg px-3 py-2.5 text-sm outline-none data-[disabled=true]:pointer-events-none data-[disabled=true]:opacity-50 data-selected:bg-accent"
                  >
                    <Building2 className="size-4 shrink-0 text-muted-foreground" />
                    <span className="truncate">
                      <HighlightText text={ws.name} query={query} />
                    </span>
                    <span className="ml-auto text-xs text-muted-foreground truncate">
                      {ws.slug}
                    </span>
                  </CommandPrimitive.Item>
                ))}
              </CommandPrimitive.Group>
            )}

            {isLoading && (
              <div className="flex items-center justify-center py-10">
                <Loader2 className="size-5 animate-spin text-muted-foreground" />
              </div>
            )}

            {!isLoading &&
              query.trim() &&
              !hasResults &&
              filteredPages.length === 0 &&
              filteredCommands.length === 0 &&
              filteredWorkspaces.length === 0 && (
                <CommandPrimitive.Empty className="py-10 text-center text-sm text-muted-foreground">
                  No results found.
                </CommandPrimitive.Empty>
              )}

            {!isLoading && results.projects.length > 0 && (
              <CommandPrimitive.Group
                heading="Projects"
                className="p-2 [&_[cmdk-group-heading]]:px-3 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:text-xs [&_[cmdk-group-heading]]:font-medium [&_[cmdk-group-heading]]:text-muted-foreground"
              >
                {results.projects.map((project) => (
                  <CommandPrimitive.Item
                    key={`project:${project.id}`}
                    value={`project:${project.id}`}
                    onSelect={handleSelect}
                    className="flex cursor-default select-none flex-col gap-1 rounded-lg px-3 py-2.5 text-sm outline-none data-[disabled=true]:pointer-events-none data-[disabled=true]:opacity-50 data-selected:bg-accent"
                  >
                    <div className="flex items-center gap-2.5">
                      <span className="size-4 shrink-0 text-center text-sm leading-4">
                        {project.icon || <FolderKanban className="size-4 text-muted-foreground" />}
                      </span>
                      <span className="truncate">
                        <HighlightText text={project.title} query={query} />
                      </span>
                      <span
                        className={`ml-auto text-xs shrink-0 ${PROJECT_STATUS_CONFIG[project.status as ProjectStatus]?.color ?? "text-muted-foreground"}`}
                      >
                        {PROJECT_STATUS_CONFIG[project.status as ProjectStatus]?.label ?? project.status}
                      </span>
                    </div>
                    {project.match_source === "description" &&
                      project.matched_snippet && (
                        <div className="flex items-start gap-2 pl-[26px]">
                          <span className="text-xs text-muted-foreground truncate">
                            <HighlightText
                              text={project.matched_snippet}
                              query={query}
                            />
                          </span>
                        </div>
                      )}
                  </CommandPrimitive.Item>
                ))}
              </CommandPrimitive.Group>
            )}

            {!isLoading && results.issues.length > 0 && (
              <CommandPrimitive.Group
                heading="Issues"
                className="p-2 [&_[cmdk-group-heading]]:px-3 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:text-xs [&_[cmdk-group-heading]]:font-medium [&_[cmdk-group-heading]]:text-muted-foreground"
              >
                {results.issues.map((issue) => (
                  <CommandPrimitive.Item
                    key={issue.id}
                    value={issue.id}
                    onSelect={handleSelect}
                    className="flex cursor-default select-none flex-col gap-1 rounded-lg px-3 py-2.5 text-sm outline-none data-[disabled=true]:pointer-events-none data-[disabled=true]:opacity-50 data-selected:bg-accent"
                  >
                    <div className="flex items-center gap-2.5">
                      <StatusIcon
                        status={issue.status}
                        className="size-4 shrink-0"
                      />
                      <span className="text-xs text-muted-foreground shrink-0">
                        {issue.identifier}
                      </span>
                      <span className="truncate">
                        <HighlightText text={issue.title} query={query} />
                      </span>
                      <span
                        className={`ml-auto text-xs shrink-0 ${STATUS_CONFIG[issue.status].iconColor}`}
                      >
                        {STATUS_CONFIG[issue.status].label}
                      </span>
                    </div>
                    {issue.match_source === "comment" &&
                      issue.matched_snippet && (
                        <div className="flex items-start gap-2 pl-[26px]">
                          <MessageSquare className="size-3 shrink-0 text-muted-foreground mt-0.5" />
                          <span className="text-xs text-muted-foreground truncate">
                            <HighlightText
                              text={issue.matched_snippet}
                              query={query}
                            />
                          </span>
                        </div>
                      )}
                  </CommandPrimitive.Item>
                ))}
              </CommandPrimitive.Group>
            )}

            {!isLoading && !query.trim() && recentIssues.length > 0 && (
              <CommandPrimitive.Group className="p-2">
                <div className="flex items-center gap-2 px-3 py-1.5 text-xs font-medium text-muted-foreground">
                  <Clock className="size-3" />
                  <span>Recent</span>
                </div>
                {recentIssues.map((item) => (
                  <CommandPrimitive.Item
                    key={item.id}
                    value={item.id}
                    onSelect={handleSelect}
                    className="flex cursor-default select-none items-center gap-2.5 rounded-lg px-3 py-2.5 text-sm outline-none data-[disabled=true]:pointer-events-none data-[disabled=true]:opacity-50 data-selected:bg-accent"
                  >
                    <StatusIcon
                      status={item.status}
                      className="size-4 shrink-0"
                    />
                    <span className="text-xs text-muted-foreground shrink-0">
                      {item.identifier}
                    </span>
                    <span className="truncate">{item.title}</span>
                    <span
                      className={`ml-auto text-xs shrink-0 ${STATUS_CONFIG[item.status]?.iconColor ?? ""}`}
                    >
                      {STATUS_CONFIG[item.status]?.label ?? ""}
                    </span>
                  </CommandPrimitive.Item>
                ))}
              </CommandPrimitive.Group>
            )}

            {!isLoading && !query.trim() && recentIssues.length === 0 && (
              <div className="px-5 py-4 text-center text-xs text-muted-foreground">
                Type to search issues and projects
              </div>
            )}
          </CommandPrimitive.List>
        </CommandPrimitive>
      </DialogContent>
    </Dialog>
  );
}
