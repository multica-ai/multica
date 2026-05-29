// Wire shape returned by the cerebro duplicate-check API
// (server/internal/handler/duplicate_check_cerebro.go). Keep in sync.

export type DuplicateMatchVerdict = "duplicate" | "related";

export interface DuplicateMatch {
  id: string;
  identifier: string;
  title: string;
  status: string;
  url: string;
  verdict: DuplicateMatchVerdict;
  reason: string;
}
