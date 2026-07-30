// Shared child-progress shape + formatting, extracted from list-row so the
// table/board/list surfaces can format progress without eagerly importing the
// row component (and its action-menu dependency chain).

export interface ChildProgress {
  done: number;
  total: number;
  archived?: number;
}

// R10 / review F4: the numerator stays ChildIssueProgress (done|cancelled per
// R4); archived children are surfaced as a trailing annotation so the
// closed-vs-completed split reads coherently ("3/5 done · 1 archived").
export function formatProgressText(progress: ChildProgress): string {
  const archived = progress.archived ?? 0;
  if (archived > 0) {
    return `${progress.done}/${progress.total} done · ${archived} archived`;
  }
  return `${progress.done}/${progress.total}`;
}
