import { createHash } from "node:crypto";
import { readFile, realpath, stat } from "node:fs/promises";
import { dirname, isAbsolute, join, relative } from "node:path";
import { z } from "zod";
import type { SourceBinding } from "../contracts/resources.js";
import { problem, VerificationError } from "../host/problem.js";
import { toJson, type Json } from "../contracts/index.js";

// Only a host-configured file is accepted; neither SQL nor paths come from a tool call.
export async function exportSQLite(
  binding: SourceBinding,
  selected: string[],
): Promise<Json> {
  if (!binding.databaseFile || !selected.length || selected.length > 100)
    throw new VerificationError(
      problem(
        "invalidInput",
        "A selected, host-authorized SQLite source is required",
      ),
    );
  const path = await realpath(binding.databaseFile),
    info = await stat(path);
  if (!info.isFile())
    throw new VerificationError(
      problem("invalidInput", "The source database is not a regular file"),
    );
  const { DatabaseSync } = await import("node:sqlite");
  const database = new DatabaseSync(path, { readOnly: true });
  let scannedBytes = 0,
    fileBytes = 0;
  const rows = (sql: string, ...params: (string | number)[]) => {
    const result: Record<string, string | number | null>[] = [];
    for (const row of database.prepare(sql).iterate(...params)) {
      scannedBytes += Buffer.byteLength(JSON.stringify(row));
      if (scannedBytes + fileBytes > 60 * 1024 * 1024)
        throw new VerificationError(
          problem(
            "coverageIncomplete",
            "The selected source exceeds its bounded export size",
          ),
        );
      result.push(row as Record<string, string | number | null>);
    }
    return result;
  };
  const json = (value: unknown) =>
    typeof value === "string" && value ? (JSON.parse(value) as Json) : null;
  let bytes = 0;
  let committed = false;
  database.exec("BEGIN");
  try {
    // Migrations are additive in practice, so check the columns this reader
    // uses rather than pinning schema versions; a removed column fails closed.
    const columns = (table: string) =>
      new Set(
        rows(`SELECT name FROM pragma_table_info('${table}')`).map((row) =>
          String(row.name),
        ),
      );
    for (const [table, required] of Object.entries(requiredColumns)) {
      const present = columns(table);
      const missing = required.filter((column) => !present.has(column));
      if (missing.length)
        throw new VerificationError(
          problem(
            "unsupportedVersion",
            `This SQLite source lacks ${table}.${missing.join(`, ${table}.`)}; use its API exporter`,
          ),
        );
    }
    // Schema 6 added durable submission state; older stores have none.
    const hasSubmissions = columns("prompt_submission").size > 0;
    const scope = rows(
      "SELECT id FROM scope WHERE api_origin = ? AND user_id = ? AND workspace_id = ?",
      binding.scope.apiOrigin,
      binding.scope.userId,
      binding.scope.workspaceId,
    )[0];
    if (!scope || typeof scope.id !== "number")
      throw new VerificationError(
        problem(
          "forbidden",
          "The selected scope does not exist in the host-bound database",
        ),
      );
    const generation = String(
      rows("SELECT value FROM store_meta WHERE key = ?", "store_generation")[0]
        ?.value ?? "",
    );
    if (!generation)
      throw new VerificationError(
        problem("unsupportedVersion", "The source has no generation identity"),
      );
    const ids = [...new Set(selected)],
      conversations = [];
    for (let index = 0; index < ids.length; index++) {
      if (ids.length > 100)
        throw new VerificationError(
          problem(
            "coverageIncomplete",
            "Relationship closure exceeds 100 conversations",
          ),
        );
      const id = ids[index]!,
        conversation = rows(
          "SELECT * FROM conversation WHERE scope_id = ? AND id = ?",
          scope.id,
          id,
        )[0];
      if (!conversation)
        throw new VerificationError(
          problem(
            "forbidden",
            "A related conversation is unavailable in the selected scope",
          ),
        );
      for (const key of ["forked_from_id", "side_source_id"]) {
        const related = conversation[key];
        if (typeof related === "string" && related && !ids.includes(related))
          ids.push(related);
      }
      const sourceTurns = rows(
        "SELECT * FROM turn WHERE conversation_id = ? ORDER BY turn_index LIMIT 20001",
        id,
      );
      if (sourceTurns.length > 20000)
        throw new VerificationError(
          problem(
            "coverageIncomplete",
            "The selected history exceeds its turn limit",
          ),
        );
      const attachmentIds = new Set<string>();
      const turns = sourceTurns.map((turn) => {
        const messages = rows(
          "SELECT * FROM message WHERE conversation_id = ? AND turn_id = ? ORDER BY created_at,id",
          id,
          String(turn.id),
        ).map((message) => {
          const attachments = z
            .array(z.string())
            .parse(json(message.attachment_ids));
          attachments.forEach((id) => attachmentIds.add(id));
          return {
            id: message.id,
            role: message.role,
            content: message.content,
            attachment_ids: attachments,
            steered: !!message.steered,
            failed: !!message.failed,
            created_at: message.created_at,
          };
        });
        const events = rows(
          "SELECT * FROM event WHERE conversation_id = ? AND turn_id = ? ORDER BY seq LIMIT 10001",
          id,
          String(turn.id),
        ).map((event) => ({
          seq: event.seq,
          type: event.type,
          item_id: event.item_id ?? undefined,
          turn_id: turn.id,
          final: !!event.final,
          payload: json(event.payload),
          created_at: event.created_at,
        }));
        if (events.length > 10000)
          throw new VerificationError(
            problem(
              "coverageIncomplete",
              "A selected turn exceeds its event limit",
            ),
          );
        return {
          id: turn.id,
          turn_index: turn.turn_index,
          request_id: turn.request_id,
          status: turn.status,
          started_at: turn.started_at,
          completed_at: turn.completed_at ?? undefined,
          messages,
          events,
        };
      });
      const queue = rows(
        "SELECT * FROM prompt_queue WHERE conversation_id = ?",
        id,
      )[0];
      const items = rows(
        "SELECT item FROM prompt_queue_item WHERE conversation_id = ? ORDER BY position",
        id,
      ).map((item) => json(item.item));
      // The same open submissions the Local API snapshot reports; items that
      // need review are not in the queue table, and the API view lists them too.
      const submissions = hasSubmissions
        ? rows(
            "SELECT request_id, state, item, revision, blocked_reason FROM prompt_submission WHERE conversation_id = ? AND state IN ('queued', 'starting', 'recovery_required') ORDER BY updated_at, request_id",
            id,
          ).map((row) => ({
            request_id: row.request_id,
            state: row.state,
            revision: row.revision,
            item: json(row.item),
            ...(row.blocked_reason
              ? { blocked_reason: row.blocked_reason }
              : {}),
          }))
        : [];
      for (const submission of submissions) {
        const item = submission.item;
        if (
          submission.state === "recovery_required" &&
          item &&
          typeof item === "object" &&
          !Array.isArray(item) &&
          !items.some(
            (queued) =>
              queued &&
              typeof queued === "object" &&
              !Array.isArray(queued) &&
              queued.requestId === submission.request_id,
          )
        )
          items.push(item);
      }
      for (const item of items) {
        if (
          item &&
          typeof item === "object" &&
          !Array.isArray(item) &&
          Array.isArray(item.attachmentIds)
        )
          for (const id of item.attachmentIds)
            if (typeof id === "string") attachmentIds.add(id);
      }
      const files = rows(
        "SELECT * FROM attachment WHERE scope_id = ? AND conversation_id = ?",
        scope.id,
        id,
      );
      files.forEach((file) => attachmentIds.add(String(file.id)));
      const attachments = [];
      const root = attachmentIds.size
        ? await realpath(binding.filesRoot ?? join(dirname(path), "files"))
        : "";
      for (const attachmentId of attachmentIds) {
        const file = rows(
          "SELECT * FROM attachment WHERE scope_id = ? AND id = ?",
          scope.id,
          attachmentId,
        )[0];
        if (!file || Number(file.size) > 25 * 1024 * 1024)
          throw new VerificationError(
            problem(
              "coverageIncomplete",
              "An attachment is missing or exceeds the import size limit",
            ),
          );
        if (
          scannedBytes + fileBytes + Math.ceil((Number(file.size) * 4) / 3) >
          60 * 1024 * 1024
        )
          throw new VerificationError(
            problem(
              "coverageIncomplete",
              "Attachments exceed the bounded export size",
            ),
          );
        const actual = await realpath(join(root, String(file.path))),
          rel = relative(root, actual);
        if (rel.startsWith("..") || isAbsolute(rel))
          throw new VerificationError(
            problem("forbidden", "An attachment escapes its source file root"),
          );
        const content = await readFile(actual),
          sha256 = createHash("sha256").update(content).digest("hex");
        if (
          content.length !== Number(file.size) ||
          (file.sha256 && file.sha256 !== sha256)
        )
          throw new VerificationError(
            problem(
              "identityMismatch",
              "Attachment integrity validation failed",
            ),
          );
        fileBytes += Math.ceil((content.length * 4) / 3);
        attachments.push({
          originalId: attachmentId,
          name: file.file_name || `${attachmentId}.bin`,
          mediaType: file.media_type,
          data: content.toString("base64"),
          sha256,
        });
      }
      const value = toJson({
        originalId: id,
        snapshot: {
          version: 2,
          store_generation: generation,
          snapshot_seq: conversation.last_seq,
          conversation: {
            ...Object.fromEntries(
              Object.entries(conversation).filter(
                ([key]) =>
                  !["provider_session_id", "scope_id", "cwd", "side"].includes(
                    key,
                  ),
              ),
            ),
            conversation_id: id,
          },
          turns,
          has_older_turns: false,
          queue: {
            revision: queue?.revision ?? 0,
            paused: !!queue?.paused,
            items,
          },
          submissions,
          forked_from_id: conversation.forked_from_id,
          forked_from_turn_id: conversation.forked_from_turn_id,
          side_context: json(conversation.side),
        },
        attachments,
      });
      bytes += Buffer.byteLength(JSON.stringify(value));
      if (bytes > 60 * 1024 * 1024)
        throw new VerificationError(
          problem(
            "coverageIncomplete",
            "The selected data exceeds its snapshot limit",
          ),
        );
      conversations.push(value);
    }
    database.exec("COMMIT");
    committed = true;
    return { schemaVersion: 1, conversations };
  } finally {
    if (!committed) database.exec("ROLLBACK");
    database.close();
  }
}

const requiredColumns: Record<string, string[]> = {
  scope: ["id", "api_origin", "user_id", "workspace_id"],
  store_meta: ["key", "value"],
  conversation: [
    "id",
    "scope_id",
    "forked_from_id",
    "forked_from_turn_id",
    "side_source_id",
    "last_seq",
    "side",
  ],
  turn: [
    "id",
    "conversation_id",
    "turn_index",
    "request_id",
    "status",
    "started_at",
    "completed_at",
  ],
  message: [
    "id",
    "conversation_id",
    "turn_id",
    "role",
    "content",
    "attachment_ids",
    "steered",
    "failed",
    "created_at",
  ],
  event: [
    "conversation_id",
    "turn_id",
    "seq",
    "type",
    "item_id",
    "final",
    "payload",
    "created_at",
  ],
  prompt_queue: ["conversation_id", "revision", "paused"],
  prompt_queue_item: ["conversation_id", "item", "position"],
  attachment: [
    "id",
    "scope_id",
    "conversation_id",
    "file_name",
    "media_type",
    "path",
    "size",
    "sha256",
  ],
};
