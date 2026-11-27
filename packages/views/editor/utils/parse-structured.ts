/**
 * Parses a `structured` attachment (JSON, JSON Lines, YAML) for the viewer's
 * tree. A failure carries the parser's own message — it names the line — so
 * the viewer can say why it fell back to the source.
 */

import { parseAllDocuments } from "yaml";
import type { StructuredFormat } from "./preview";

export type StructuredParse =
  | { ok: true; value: unknown }
  | { ok: false; message: string };

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function parseJsonLines(text: string): StructuredParse {
  const values: unknown[] = [];
  const lines = text.split(/\r\n|\n|\r/);
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]!;
    if (line.trim() === "") continue;
    try {
      values.push(JSON.parse(line));
    } catch (error) {
      return { ok: false, message: `Line ${i + 1}: ${errorMessage(error)}` };
    }
  }
  return { ok: true, value: values };
}

function parseYaml(text: string): StructuredParse {
  // Every document in the stream: a multi-document file (`---` separated,
  // e.g. Kubernetes manifests) shows as a list of its documents.
  const documents = parseAllDocuments(text);
  const values: unknown[] = [];
  for (const document of documents) {
    const [firstError] = document.errors;
    if (firstError) return { ok: false, message: firstError.message };
    // toJS enforces yaml's alias expansion cap, so a "billion laughs" file
    // throws here instead of exhausting memory.
    values.push(document.toJS());
  }
  if (values.length === 0) return { ok: true, value: null };
  return { ok: true, value: values.length === 1 ? values[0] : values };
}

export function parseStructured(
  text: string,
  format: StructuredFormat,
): StructuredParse {
  try {
    switch (format) {
      case "json":
        return { ok: true, value: JSON.parse(text) };
      case "jsonl":
        return parseJsonLines(text);
      case "yaml":
        return parseYaml(text);
    }
  } catch (error) {
    return { ok: false, message: errorMessage(error) };
  }
}
