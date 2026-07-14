// Data model for the Dev/Profiling-only performance flight recorder.
//
// Every field here is metadata: timings, counts, enums, and URLs already
// stripped of query/hash/credentials at the collector entry. There is no field
// that can hold page text, input values, React props/state, request bodies,
// headers, tokens, or any content snapshot — that constraint is enforced by
// construction, not by a redaction pass (see MUL-4466 Standard mode).

export type Surface = "web" | "desktop-renderer";
export type RecorderMode = "development" | "profiling";

export interface HostConfig {
  /** App version string shown in the report header. Non-sensitive. */
  appVersion: string;
  surface: Surface;
  mode: RecorderMode;
  /** Optional threshold overrides; defaults applied when omitted. */
  thresholds?: Partial<Thresholds>;
  /**
   * Stable React Profiler boundary IDs the host has registered. Only commits
   * whose Profiler `id` is in this set are attributed by name; everything else
   * is recorded anonymously. Fail-closed: unset means "nothing registered".
   */
  boundaryAllowlist?: readonly string[];
  /**
   * Stable `data-testid` values the host has registered. Only these are emitted
   * as the interaction target; any other testid is dropped.
   */
  testIdAllowlist?: readonly string[];
}

export interface Thresholds {
  /** rAF gap (ms) above which a frame is considered dropped when LoAF absent. */
  frameGapMs: number;
  /** Boundary actualDuration (ms) above which a React commit is an incident. */
  reactCommitMs: number;
  /** Resource duration (ms) above which a request is slow. */
  resourceMs: number;
  /** Interaction total (ms) above which an interaction is flagged. */
  interactionMs: number;
}

export interface Capabilities {
  longTask: boolean;
  longAnimationFrame: boolean;
  eventTiming: boolean;
  reactCommit: boolean;
  resourceTiming: boolean;
  mutationObserver: boolean;
}

export type InteractionType = "click" | "scroll" | "input" | "navigation" | "background";

export interface RouteInfo {
  /** Sanitized origin, or null when the URL could not be parsed. */
  origin: string | null;
  pathname: string | null;
}

export interface InteractionInfo {
  type: InteractionType;
  /** Only a host-registered, stable data-testid; never other identifiers. */
  testId?: string;
}

export type EvidenceKind =
  | "react_commit"
  | "long_task"
  | "frame"
  | "resource"
  | "interaction"
  | "insufficient";

export interface ReactCommitEvidence {
  /** Present only when the committed boundary is host-registered. */
  boundaryId?: string;
  phase: "mount" | "update" | "nested-update" | "unknown";
  actualDurationMs: number;
}

export interface LongTaskEvidence {
  startOffsetMs: number;
  durationMs: number;
}

export interface FrameEvidence {
  startOffsetMs: number;
  durationMs: number;
  source: "loaf" | "raf";
}

export interface ResourceEvidence {
  origin: string | null;
  pathname: string | null;
  initiatorType: string;
  durationMs: number;
  startOffsetMs: number;
}

export interface Incident {
  id: string;
  offsetMs: number;
  route: RouteInfo;
  interaction: InteractionInfo;
  totalDurationMs: number;
  primaryEvidence: EvidenceKind;
  mutationCount: number;
  reactCommits: ReactCommitEvidence[];
  longTasks: LongTaskEvidence[];
  frames: FrameEvidence[];
  resources: ResourceEvidence[];
}

export interface ReportHost {
  appVersion: string;
  surface: Surface;
  mode: RecorderMode;
}

export interface Report {
  schemaVersion: string;
  recorderVersion: string;
  host: ReportHost;
  capabilities: Capabilities;
  thresholdsMs: Thresholds;
  session: {
    durationMs: number;
    incidentCount: number;
  };
  incidents: Incident[];
}

export const SCHEMA_VERSION = "1.0";
export const RECORDER_VERSION = "0.1.0";

export const DEFAULT_THRESHOLDS: Thresholds = {
  frameGapMs: 50,
  reactCommitMs: 16.7,
  resourceMs: 500,
  interactionMs: 200,
};
