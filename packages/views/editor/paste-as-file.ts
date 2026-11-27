/**
 * Character count above which a plain-text paste is uploaded as a
 * `pasted-text.txt` attachment instead of being inserted as body text.
 *
 * Chat is the only editor that opts in; see `pasteAsFileThreshold` in
 * `content-editor.tsx` for why. The constant stays separate from that call
 * site so any composer that later joins converts at the same point — two
 * surfaces disagreeing would let the same content be a file on the way in and
 * text on the way back out.
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
