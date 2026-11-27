import { z } from "zod";
import type { TrajectorySelection } from "./web-entry";
const uuidSchema = z.string().regex(/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/);
const kindSchema = z.string().regex(/^[a-z][a-z0-9_]{0,63}$/);
const int32Schema = z.number().int().min(-2147483648).max(2147483647);
export const trajectoryLinkKeys = ["trajectory", "target_kind", "target_id", "target_issue_id", "seq"] as const;
export function parseTrajectoryLink(params: URLSearchParams): {
    requested: boolean;
    selection: TrajectorySelection | null;
    error: string | null;
} {
    const enabled = params.get("trajectory");
    if (enabled === null)
        return { requested: false, selection: null, error: null };
    const kind = params.get("target_kind"), id = params.get("target_id"), rawSeq = params.get("seq"), issueId = params.get("target_issue_id");
    if (enabled !== "1" || trajectoryLinkKeys.some(key => params.getAll(key).length > 1))
        return { requested: false, selection: null, error: "invalid_link" };
    if (kind === null && id === null && rawSeq === null && issueId === null)
        return { requested: true, selection: null, error: null };
    if (!kindSchema.safeParse(kind).success || !uuidSchema.safeParse(id).success || issueId !== null && (!uuidSchema.safeParse(issueId).success || kind === "issue" && issueId !== id) || rawSeq !== null && (!/^-?(0|[1-9]\d*)$/.test(rawSeq) || !int32Schema.safeParse(Number(rawSeq)).success || kind !== "run"))
        return { requested: false, selection: null, error: "invalid_link" };
    return { requested: true, selection: { kind: kind!, id: id!, ...(issueId !== null ? { issueId } : {}), ...(rawSeq !== null ? { seq: Number(rawSeq) } : {}) }, error: null };
}
