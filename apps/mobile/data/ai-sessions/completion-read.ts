import type { LocalV2Snapshot } from "@multica/core/local-conversation/v2";
import type { ConversationRow } from "./projection";

/** Only an actually visible successful result in the active screen is a read. */
export function shownCompletion(snapshot: LocalV2Snapshot | undefined, rows: readonly ConversationRow[], visibleRows: readonly string[], active: boolean) {
  const completion = snapshot?.conversation.completion;
  const read = snapshot?.conversation.read_completion_seq;
  if (!active || !snapshot || !completion || read === undefined || read >= completion.completion_seq) return null;
  if (!snapshot.turns.some((turn) => turn.id === completion.turn_id && turn.turn_index === completion.turn_index && turn.status === "completed")) return null;
  const result = rows.filter((row) => row.turnId === completion.turn_id && row.role === "assistant").at(-1);
  if (!result || result.role !== "assistant" || !result.content.trim() || result.running || !visibleRows.includes(result.id)) return null;
  return { store_generation: snapshot.store_generation, ...completion };
}
