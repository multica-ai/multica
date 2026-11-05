/** Squad rooms on one floor: a 3 x 2 grid, slots 0 to 5. Mirrors the server. */
export const COMPANY_SLOTS = 6;

/**
 * The boss's desk in each floor's product office: the slot after the squad rooms. The secretary
 * sits there on every floor. Mirrors the engine's boss room.
 */
export const COMPANY_BOSS_SLOT = COMPANY_SLOTS;

/** Desks in one squad room (a 5 x 2 pod). Agents past this show as a `+N` overflow. */
export const COMPANY_ROOM_DESKS = 10;

/** Floors a workspace can have. Mirrors the server cap. */
export const COMPANY_MAX_FLOORS = 16;

/** Longest floor name, in characters. Mirrors the server and column rule. */
export const COMPANY_FLOOR_NAME_MAX_LENGTH = 64;
