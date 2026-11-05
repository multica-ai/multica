import type { Json } from "../contracts/index.js";
export function contentManifest(content: Json) {
  const entities: { type: string; originalId: string }[] = [];
  const relationships: { from: string; to: string; kind: string }[] = [];
  const attachments: {
    originalId: string;
    relativePath: string;
    bytes: number;
    sha256: string;
  }[] = [];
  const root =
    content && !Array.isArray(content) && typeof content === "object"
      ? content
      : {};
  const object = (value: Json | undefined): Record<string, Json> =>
    value && !Array.isArray(value) && typeof value === "object" ? value : {};
  const array = (value: Json | undefined): Json[] =>
    Array.isArray(value) ? value : [];
  const add = (type: string, id: Json | undefined) => {
    if (typeof id === "string") entities.push({ type, originalId: id });
  };
  const relate = (
    from: Json | undefined,
    to: Json | undefined,
    kind: string,
  ) => {
    if (typeof from === "string" && typeof to === "string")
      relationships.push({ from, to, kind });
  };
  const file = (value: Record<string, Json>, path: string) => {
    const id = value.originalId ?? value.id;
    if (
      typeof id === "string" &&
      typeof value.data === "string" &&
      typeof value.sha256 === "string"
    ) {
      const entry = {
        originalId: id,
        relativePath: path,
        bytes: Buffer.from(value.data, "base64").length,
        sha256: value.sha256,
      };
      const prior = attachments.find((item) => item.originalId === id);
      if (
        prior &&
        (prior.sha256 !== entry.sha256 || prior.bytes !== entry.bytes)
      )
        throw new Error("Conflicting snapshot attachment identity");
      if (!prior) attachments.push(entry);
    }
  };
  array(root.conversations).forEach((entry, index) => {
    const conversation = object(entry),
      snapshot = object(conversation.snapshot),
      id = conversation.originalId;
    add("conversation", id);
    for (const key of ["forked_from_id", "side_source_id"])
      relate(id, object(snapshot.conversation)[key], key);
    for (const value of array(snapshot.turns)) {
      const turn = object(value);
      add("turn", turn.id);
      relate(turn.id, id, "conversation");
      for (const value of array(turn.messages)) {
        const message = object(value);
        add("message", message.id);
        relate(message.id, turn.id, "turn");
        for (const attachment of array(message.attachment_ids))
          relate(message.id, attachment, "attachment");
      }
    }
    array(conversation.attachments).forEach((value, fileIndex) =>
      file(
        object(value),
        `content/conversations/${index}/attachments/${fileIndex}`,
      ),
    );
  });
  add("workspace", object(root.workspace).id);
  for (const kind of [
    "statuses",
    "workflows",
    "projects",
    "issues",
    "comments",
    "labels",
  ]) {
    for (const value of array(root[kind])) {
      const row = object(value);
      add(kind, row.id);
      for (const key of [
        "project_id",
        "parent_issue_id",
        "issue_id",
        "parent_id",
        "workflow_id",
        "workspace_id",
      ])
        relate(row.id, row[key], key);
    }
  }
  array(root.attachments).forEach((value, index) =>
    file(object(value), `content/attachments/${index}`),
  );
  return { entities, relationships, attachments, missing: [] as string[] };
}
