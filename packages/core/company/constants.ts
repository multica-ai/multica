/** Squad rooms on one floor: a 3 x 2 grid, slots 0 to 5. Mirrors the server. */
export const COMPANY_SLOTS = 6;

/** Desks in one squad room (a 5 x 2 pod). Agents past this show as a `+N` overflow. */
export const COMPANY_ROOM_DESKS = 10;

/** Floors a workspace can have. Mirrors the server cap. */
export const COMPANY_MAX_FLOORS = 16;

/** Longest floor name, in characters. Mirrors the server and column rule. */
export const COMPANY_FLOOR_NAME_MAX_LENGTH = 64;
