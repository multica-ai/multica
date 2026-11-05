import type { CSSProperties } from "react";

/**
 * Stable identity colour for each comment author.
 *
 * Every member and agent gets one of the `--participant-N` tokens. The colour
 * is keyed by actor id, so it survives avatar and display-name changes and is
 * the same on every device. Colour is a secondary cue — the avatar and name
 * still identify the author.
 */
export const PARTICIPANT_COLOR_COUNT = 8;

export type ParticipantKey = { type: string; id: string };

function keyOf(type: string, id: string): string {
  return `${type}:${id}`;
}

export function participantColorIndex(actorType: string, actorId: string): number {
  // FNV-1a: small, deterministic and well spread for short id strings.
  let hash = 0x811c9dc5;
  const key = keyOf(actorType, actorId);
  for (let i = 0; i < key.length; i++) {
    hash ^= key.charCodeAt(i);
    hash = Math.imul(hash, 0x01000193);
  }
  return (hash >>> 0) % PARTICIPANT_COLOR_COUNT;
}

/**
 * Assigns palette slots to a workspace roster in order: the first participant
 * gets slot 0, the next slot 1, and so on. The palette is ordered so early
 * slots are far apart on the hue wheel, which a hash cannot guarantee (it put
 * amber and red, 45° apart, on the two most active people). The roster must be
 * in a stable order (oldest first) so someone joining later never recolours
 * the people already there. Past PARTICIPANT_COLOR_COUNT people, colours
 * repeat by hash.
 */
export function assignParticipantColors(roster: readonly ParticipantKey[]): Map<string, number> {
  const slots = new Map<string, number>();
  for (const p of roster) {
    const key = keyOf(p.type, p.id);
    if (slots.has(key)) continue;
    slots.set(key, slots.size < PARTICIPANT_COLOR_COUNT ? slots.size : participantColorIndex(p.type, p.id));
  }
  return slots;
}

/** `var(--participant-N)` for the author; undefined for system/unknown actors. */
export function participantColor(
  actorType: string | null | undefined,
  actorId: string | null | undefined,
  slots?: ReadonlyMap<string, number>,
): string | undefined {
  if ((actorType !== "member" && actorType !== "agent") || !actorId) return undefined;
  const slot = slots?.get(keyOf(actorType, actorId)) ?? participantColorIndex(actorType, actorId);
  return `var(--participant-${slot})`;
}

/** Exposes the author's colour to descendants as `--participant`. */
export function participantStyle(color: string | undefined): CSSProperties | undefined {
  return color ? ({ "--participant": color } as CSSProperties) : undefined;
}
