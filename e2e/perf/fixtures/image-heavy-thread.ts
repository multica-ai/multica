/** Real 2x Chromium captures of the app; see screenshots/README.md. */
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

export const FIXTURE_VERSION = "2.0.0";
export const CASE = process.env.PERF_IMAGE_CASE ?? "in-viewport";
export const NO_IMAGES = process.env.PERF_NO_IMAGES === "1";
export const CAPTURE = process.env.PERF_CAPTURE_SCREENSHOTS === "1";
export const SHOTS = [
 { filename: "issue-2560x1720.png", width: 2560, height: 1720 },
 { filename: "thread-2600x2160.png", width: 2600, height: 2160 },
];

export const WORKSPACE_SLUG = "perf";
export const WORKSPACE_ID = "11111111-1111-4111-8111-111111111111";
export const USER_ID = "22222222-2222-4222-8222-222222222222";
export const ISSUE_ID = "33333333-3333-4333-8333-333333333333";
export const ISSUE_IDENTIFIER = "PERF-1";

export const REPLY_COMMENT_COUNT = 36;
/** Every fourth reply carries a second screenshot. */
export const IMAGE_COUNT = 1 + REPLY_COMMENT_COUNT + Math.ceil(REPLY_COMMENT_COUNT / 4);
export const TYPING_TEXT = "abcdefghijklmnopqrstuvwxyz".repeat(7).slice(0, 172);
export const KEY_DELAY_MS = 25;
export const MEDIA_PREFIX = "/uploads/perf/";

const uuid = (prefix: string, n: number) =>
  `${prefix}${String(n).padStart(4, "0")}-4444-4444-8444-444444444444`.slice(0, 36);

export const commentIds = Array.from({ length: 1 + REPLY_COMMENT_COUNT }, (_, i) => uuid("cccc", i));
export const rootCommentId = commentIds[0]!;

const at = (offsetSeconds: number) =>
  new Date(Date.UTC(2026, 9, 1, 0, 0, 0) + offsetSeconds * 1000).toISOString();

function attachment(commentId: string, n: number) {
  const id = uuid("eeee", n);
  const key = `${MEDIA_PREFIX}${id}.png`;
  const shot = SHOTS[n % SHOTS.length]!;
  return {
    id,
    workspace_id: WORKSPACE_ID,
    issue_id: ISSUE_ID,
    comment_id: commentId,
    chat_session_id: null,
    chat_message_id: null,
    uploader_type: "member",
    uploader_id: USER_ID,
    filename: `screenshot-${n}.png`,
    url: key,
    download_url: key,
    markdown_url: key,
    content_type: "image/png",
    size_bytes: CAPTURE ? 0 : mediaBytes(n).length,
    ...(CASE === "without-dimensions" ? {} : { width: shot.width, height: shot.height }),
    created_at: at(n),
  };
}

export function buildTimelineEntries() {
  let image = 0;
  return commentIds.map((id, index) => {
    const files = NO_IMAGES || CAPTURE ? [] : Array.from({ length: index > 0 && index % 4 === 1 ? 2 : 1 }, () => attachment(id, image++));
    const prose = Array.from(
      { length: 3 },
      (_, p) => `Paragraph ${p + 1} of comment ${index}: what changed, and the screenshot that shows it.`,
    );
    return {
      type: "comment" as const,
      id,
      actor_type: "user" as const,
      actor_id: USER_ID,
      created_at: at(index * 60),
      updated_at: at(index * 60),
      issue_id: ISSUE_ID,
      parent_id: index === 0 ? null : rootCommentId,
      content: [...prose, ...files.map((f) => `![${f.filename}](${f.url})`)].join("\n\n"),
      resolved_at: null,
      reactions: [],
      attachments: files,
      source_task_id: null,
    };
  });
}

const media = new Map<string, Buffer>();
export function mediaBytes(n: number): Buffer {
 const shot = SHOTS[n % SHOTS.length]!;
 let bytes = media.get(shot.filename);
 if (!bytes) {
  bytes = readFileSync(fileURLToPath(new URL(`./screenshots/${shot.filename}`, import.meta.url)));
  media.set(shot.filename, bytes);
 }
 return bytes;
}

export function fixtureDigest() {
 const hash = createHash("sha256").update(JSON.stringify({ version: FIXTURE_VERSION, case: CASE, no_images: NO_IMAGES, timeline: buildTimelineEntries(), typing: TYPING_TEXT }));
 let bytes = 0;
 if (!CAPTURE) for (let i = 0; i < SHOTS.length; i++) { const data = mediaBytes(i); hash.update(data); bytes += data.length; }
 return { version: FIXTURE_VERSION, sha256: hash.digest("hex"), bytes };
}
