// User-facing limits enforced symmetrically on the front-end (UI counter +
// disabled save) and the back-end (handler validation + DB CHECK constraint).
// Kept in core so both apps and the test suite read from one source.
export const AGENT_DESCRIPTION_MAX_LENGTH = 255;

export const AGENT_STARTER_PROMPTS_MAX = 3;
export const AGENT_STARTER_PROMPT_LABEL_MAX_LENGTH = 80;
export const AGENT_STARTER_PROMPT_MAX_LENGTH = 4000;

// Valid range for the per-agent scheduler cap. Kept here so creation,
// duplication, and settings editing cannot silently drift.
export const AGENT_MAX_CONCURRENT_TASKS_MIN = 1;
export const AGENT_MAX_CONCURRENT_TASKS_MAX = 50;
