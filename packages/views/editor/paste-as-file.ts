/**
 * Character count above which a plain-text paste into chat is uploaded as a
 * `pasted-text.txt` attachment instead of being inserted as body text.
 *
 * Chat and issue comments opt in at different points; see
 * `pasteAsFileThreshold` in `content-editor.tsx` for why. Each value lives
 * here rather than at its call sites so every editor of one surface converts
 * at the same point — two surfaces disagreeing would let the same content be a
 * file on the way in and text on the way back out.
 *
 * The number is a product judgement, not a technical limit: nothing in this
 * stack caps message length (no client schema max, the server only rejects
 * empty content, and every column is unbounded TEXT). Below ~4k a paste is
 * still scrollable as text; above it the body stops being readable and the
 * attachment is the better container. For reference, ChatGPT converts at
 * 10,000 characters and Discord at its 2,000-character message ceiling.
 *
 * NOT the same thing as `LARGE_PASTE_TEXT_THRESHOLD` in
 * `extensions/markdown-paste.ts` — that one (50,000) is a parser bail-out that
 * keeps a huge paste from running the markdown pipeline, and applies to every
 * editor including the ones that never convert to a file.
 */
export const PASTE_AS_FILE_THRESHOLD = 4000;

/**
 * Character count above which a plain-text paste into an issue comment — new
 * comment, reply, or comment edit — is uploaded as a `pasted-text.txt`
 * attachment.
 *
 * Far above chat's value on purpose. A comment is prose a human reads in the
 * thread, so a long write-up, a stack trace or a sizeable snippet stays
 * inline. What this catches is the machine-sized paste — a log dump, a JSON
 * export, a minified bundle — that no one reads in place and that the comment
 * renderer has to lay out in full for everyone who opens the issue. Nothing
 * else bounds a comment: there is no client or server length limit, so
 * without this a single paste of a megabyte-sized line goes straight into
 * the body.
 *
 * The value matches `LARGE_PASTE_TEXT_THRESHOLD` in
 * `extensions/markdown-paste.ts`: that is where the editor already stops
 * treating a paste as markdown prose and inserts it as literal text, so above
 * it the paste is not something a person wrote for the thread.
 */
export const COMMENT_PASTE_AS_FILE_THRESHOLD = 50_000;
