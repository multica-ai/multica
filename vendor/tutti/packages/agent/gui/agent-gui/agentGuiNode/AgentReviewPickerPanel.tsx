import { menuItemClassName } from "@tutti-os/ui-system";
import { observer } from "mobx-react-lite";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { cn } from "../../app/renderer/lib/utils";
import {
  AgentReviewBranchController,
  type AgentReviewBranchState
} from "./AgentReviewBranchController";

type ReviewStage = "root" | "base" | "commit" | "custom";

interface ReviewMenuEntry {
  key: string;
  label: string;
  description?: string;
  disabled?: boolean;
  onSelect: () => void;
}

export interface AgentReviewPickerLabels {
  title: string;
  searchPlaceholder: string;
  noResults: string;
  uncommitted: string;
  baseBranch: string;
  commit: string;
  custom: string;
  branchPlaceholder: string;
  branchLoading: string;
  branchEmpty: string;
  commitPlaceholder: string;
  customPlaceholder: string;
  submit: string;
}

export interface AgentReviewPickerGitBranches {
  branches: readonly string[];
  currentBranch?: string | null;
}

const reviewMenuStyles = {
  panel:
    "agent-gui-node__slash-status-panel nodrag flex max-h-[280px] flex-col gap-1 overflow-hidden [-webkit-app-region:no-drag]",
  search:
    "nodrag h-8 w-full shrink-0 rounded-[6px] border-0 bg-transparent px-2.5 text-[11px] leading-4 text-[var(--text-primary)] placeholder:text-[var(--text-secondary)] focus-visible:outline-none [-webkit-app-region:no-drag]",
  list: "flex min-h-0 flex-1 flex-col gap-1 overflow-y-auto",
  option: cn(
    menuItemClassName,
    "nodrag min-h-9 w-full min-w-0 justify-start overflow-hidden rounded-[6px] border-0 bg-transparent px-2.5 py-2 text-left hover:bg-[var(--transparency-block)] focus:bg-[var(--transparency-block)] focus-visible:outline-none data-[highlighted]:bg-[var(--transparency-block)] active:bg-[var(--transparency-active)] disabled:pointer-events-none disabled:opacity-50"
  ),
  copy: "flex min-w-0 flex-1 items-baseline gap-1 overflow-hidden leading-[16px]",
  name: "min-w-0 shrink-0 truncate text-[11px] font-semibold text-[var(--text-primary)]",
  description:
    "min-w-0 flex-1 truncate text-[11px] font-normal text-[var(--text-secondary)]",
  message:
    "select-none px-2.5 py-2 text-[11px] leading-4 text-[var(--text-secondary)]"
};

export const AgentReviewPickerPanel = observer(function AgentReviewPickerPanel({
  labels,
  onRequestGitBranches,
  onSubmitReview,
  onClose
}: {
  labels: AgentReviewPickerLabels;
  onRequestGitBranches?: (() => Promise<AgentReviewPickerGitBranches>) | null;
  onSubmitReview: (command: string) => void;
  onClose: () => void;
}): React.JSX.Element {
  const [stage, setStage] = useState<ReviewStage>("root");
  const [query, setQuery] = useState("");
  const [highlightedIndex, setHighlightedIndex] = useState(0);
  const searchInputRef = useRef<HTMLInputElement | null>(null);
  const highlightedOptionRef = useRef<HTMLButtonElement | null>(null);

  const reviewBranchControllerRef = useRef<AgentReviewBranchController | null>(
    null
  );
  const [branchState, setBranchState] = useState<AgentReviewBranchState>({
    status: "idle",
    branches: [],
    currentBranch: null,
    error: null
  });

  useEffect(() => {
    const controller = new AgentReviewBranchController();
    reviewBranchControllerRef.current = controller;
    const unsubscribe = controller.subscribe(setBranchState);
    return () => {
      unsubscribe();
      controller.dispose();
      reviewBranchControllerRef.current = null;
    };
  }, []);

  useEffect(() => {
    const controller = reviewBranchControllerRef.current;
    if (!controller) {
      return;
    }
    controller.setLoader(onRequestGitBranches ?? null);
    if (stage === "base") {
      controller.ensureLoaded();
    }
  }, [onRequestGitBranches, stage]);

  useEffect(() => {
    searchInputRef.current?.focus();
  }, [stage]);

  const submit = useCallback(
    (command: string): void => {
      onSubmitReview(command);
    },
    [onSubmitReview]
  );

  const goToStage = useCallback((next: ReviewStage): void => {
    setStage(next);
    setQuery("");
    setHighlightedIndex(0);
  }, []);

  const goBackToRoot = useCallback((): void => {
    setStage("root");
    setQuery("");
    setHighlightedIndex(0);
  }, []);

  const trimmedQuery = query.trim();
  const normalizedQuery = trimmedQuery.toLowerCase();

  const entries = useMemo<ReviewMenuEntry[]>(() => {
    if (stage === "root") {
      const options: ReviewMenuEntry[] = [
        {
          key: "uncommitted",
          label: labels.uncommitted,
          onSelect: () => submit("/review uncommitted")
        },
        {
          key: "base",
          label: labels.baseBranch,
          onSelect: () => goToStage("base")
        },
        {
          key: "commit",
          label: labels.commit,
          onSelect: () => goToStage("commit")
        },
        {
          key: "custom",
          label: labels.custom,
          onSelect: () => goToStage("custom")
        }
      ];
      if (normalizedQuery === "") {
        return options;
      }
      return options.filter((option) =>
        option.label.toLowerCase().includes(normalizedQuery)
      );
    }
    if (stage === "base") {
      const branches =
        normalizedQuery === ""
          ? branchState.branches
          : branchState.branches.filter((name) =>
              name.toLowerCase().includes(normalizedQuery)
            );
      return branches.map((name) => ({
        key: `branch:${name}`,
        label: name,
        onSelect: () => submit(`/review base:${name}`)
      }));
    }
    const prefix = stage === "commit" ? "commit" : "custom";
    return [
      {
        key: `${prefix}-confirm`,
        label: labels.submit,
        description: trimmedQuery || undefined,
        disabled: trimmedQuery === "",
        onSelect: () => {
          if (trimmedQuery !== "") {
            submit(`/review ${prefix}:${trimmedQuery}`);
          }
        }
      }
    ];
  }, [
    stage,
    normalizedQuery,
    trimmedQuery,
    branchState.branches,
    labels.uncommitted,
    labels.baseBranch,
    labels.commit,
    labels.custom,
    labels.submit,
    submit,
    goToStage
  ]);

  const safeHighlightedIndex =
    entries.length === 0 ? -1 : Math.min(highlightedIndex, entries.length - 1);

  useEffect(() => {
    highlightedOptionRef.current?.scrollIntoView({ block: "nearest" });
  }, [safeHighlightedIndex]);

  const searchPlaceholder =
    stage === "base"
      ? labels.branchPlaceholder
      : stage === "commit"
        ? labels.commitPlaceholder
        : stage === "custom"
          ? labels.customPlaceholder
          : labels.searchPlaceholder;

  let emptyMessage = labels.noResults;
  if (stage === "base") {
    if (branchState.status === "loading") {
      emptyMessage = labels.branchLoading;
    } else if (branchState.branches.length === 0) {
      emptyMessage = labels.branchEmpty;
    }
  }

  const handleKeyDown = useCallback(
    (event: React.KeyboardEvent<HTMLInputElement>): void => {
      if (event.key === "ArrowDown") {
        event.preventDefault();
        setHighlightedIndex((current) =>
          entries.length === 0 ? 0 : Math.min(current + 1, entries.length - 1)
        );
        return;
      }
      if (event.key === "ArrowUp") {
        event.preventDefault();
        setHighlightedIndex((current) => Math.max(current - 1, 0));
        return;
      }
      if (event.key === "Enter") {
        event.preventDefault();
        event.stopPropagation();
        const entry = entries[safeHighlightedIndex];
        if (entry && !entry.disabled) {
          entry.onSelect();
        }
        return;
      }
      if (event.key === "Escape") {
        event.preventDefault();
        event.stopPropagation();
        if (stage === "root") {
          onClose();
        } else {
          goBackToRoot();
        }
        return;
      }
      if (event.key === "Backspace" && query === "" && stage !== "root") {
        event.preventDefault();
        goBackToRoot();
      }
    },
    [entries, safeHighlightedIndex, stage, query, onClose, goBackToRoot]
  );

  return (
    <section
      className={reviewMenuStyles.panel}
      data-testid="agent-gui-review-picker-panel"
      role="dialog"
      aria-label={labels.title}
    >
      <input
        ref={searchInputRef}
        type="search"
        className={reviewMenuStyles.search}
        value={query}
        placeholder={searchPlaceholder}
        aria-label={searchPlaceholder}
        onChange={(event) => {
          setQuery(event.target.value);
          setHighlightedIndex(0);
        }}
        onKeyDown={handleKeyDown}
      />
      <div
        className={reviewMenuStyles.list}
        role="listbox"
        aria-label={labels.title}
      >
        {entries.length === 0 ? (
          <div className={reviewMenuStyles.message}>{emptyMessage}</div>
        ) : (
          entries.map((entry, index) => {
            const isHighlighted = index === safeHighlightedIndex;
            return (
              <button
                key={entry.key}
                ref={isHighlighted ? highlightedOptionRef : null}
                type="button"
                className={cn(
                  reviewMenuStyles.option,
                  isHighlighted && "bg-[var(--transparency-block)]"
                )}
                role="option"
                aria-selected={isHighlighted}
                data-highlighted={isHighlighted ? "" : undefined}
                disabled={entry.disabled}
                onMouseEnter={() => setHighlightedIndex(index)}
                onMouseDown={(event) => event.preventDefault()}
                onClick={() => {
                  if (!entry.disabled) {
                    entry.onSelect();
                  }
                }}
              >
                <span className={reviewMenuStyles.copy}>
                  <span className={reviewMenuStyles.name}>{entry.label}</span>
                  {entry.description ? (
                    <span className={reviewMenuStyles.description}>
                      {entry.description}
                    </span>
                  ) : null}
                </span>
              </button>
            );
          })
        )}
      </div>
    </section>
  );
});
