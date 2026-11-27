/**
 * CSV / TSV parsing for the attachment viewer's table.
 *
 * RFC 4180 with the usual leniency: a quote is only special at the start of a
 * field, `""` inside a quoted field is a literal quote, quoted fields may hold
 * delimiters and line breaks, and CRLF / LF / CR all end a record. Anything a
 * strict parser would reject (text after a closing quote, an unterminated
 * quote) is kept as text instead — a preview shows what is in the file rather
 * than refusing it.
 */

export type CellDelimiter = "," | ";" | "\t";

export interface ParsedTable {
  /** The first record. Padded to `columnCount`. */
  header: string[];
  /** Every later record, each padded to `columnCount`. */
  rows: string[][];
  /** The widest record's field count — ragged files keep every value. */
  columnCount: number;
}

const CANDIDATES: readonly CellDelimiter[] = [",", ";", "\t"];

/**
 * The delimiter a `.csv` file actually uses. Spreadsheets in locales with a
 * decimal comma export `;`, and a mislabeled TSV is common. Read off the first
 * logical record under each candidate. Prefer well-formed quoting, then the
 * most fields, with candidate order breaking ties. If every interpretation
 * with separators has malformed quotes, keep the parser's lenient behavior.
 * Track all candidates in one pass without allocating parsed fields.
 */
export function sniffDelimiter(text: string): CellDelimiter {
  const candidates = CANDIDATES.map((delimiter) => ({
    delimiter,
    count: 0,
    quoted: false,
    fieldStart: true,
    afterQuote: false,
    escapedQuote: false,
    wellFormed: true,
    done: false,
  }));
  let remaining = candidates.length;
  for (let i = text.charCodeAt(0) === 0xfeff ? 1 : 0; i < text.length; i += 1) {
    const char = text[i];
    for (const candidate of candidates) {
      if (candidate.done) continue;
      if (candidate.quoted) {
        if (candidate.escapedQuote) {
          candidate.escapedQuote = false;
        } else if (char === '"') {
          if (text[i + 1] === '"') candidate.escapedQuote = true;
          else {
            candidate.quoted = false;
            candidate.afterQuote = true;
          }
        }
        continue;
      }
      if (char === candidate.delimiter) {
        candidate.count += 1;
        candidate.fieldStart = true;
        candidate.afterQuote = false;
      } else if (char === "\r" || char === "\n") {
        candidate.done = true;
        remaining -= 1;
      } else if (char === '"' && candidate.fieldStart) {
        candidate.quoted = true;
        candidate.fieldStart = false;
      } else {
        if (char === '"' || candidate.afterQuote) candidate.wellFormed = false;
        candidate.fieldStart = false;
      }
    }
    if (remaining === 0) break;
    if (candidates.every((candidate) => candidate.done || candidate.quoted)) {
      // Only a quote can change any remaining interpretation. Skip the quoted
      // content together, including a large body after an unterminated quote.
      const nextQuote = text.indexOf('"', i + 1);
      if (nextQuote < 0) break;
      i = nextQuote - 1;
    }
  }

  let best: CellDelimiter = ",";
  let bestCount = 0;
  let bestWellFormed: CellDelimiter = ",";
  let bestWellFormedCount = 0;
  for (const candidate of candidates) {
    if (candidate.count > bestCount) {
      best = candidate.delimiter;
      bestCount = candidate.count;
    }
    if (
      candidate.wellFormed &&
      !candidate.quoted &&
      candidate.count > bestWellFormedCount
    ) {
      bestWellFormed = candidate.delimiter;
      bestWellFormedCount = candidate.count;
    }
  }
  return bestWellFormedCount > 0 ? bestWellFormed : best;
}

export function parseDelimited(text: string, delimiter: CellDelimiter): string[][] {
  const records: string[][] = [];
  let record: string[] = [];
  let field = "";
  let i = text.charCodeAt(0) === 0xfeff ? 1 : 0;
  const length = text.length;

  const endField = () => {
    record.push(field);
    field = "";
  };
  const endRecord = () => {
    endField();
    // A blank line is spacing, not a row of one empty cell.
    if (record.length > 1 || record[0] !== "") records.push(record);
    record = [];
  };

  while (i < length) {
    const char = text[i];

    if (char === '"' && field === "") {
      // Quoted field: read to the closing quote, unescaping `""`.
      i += 1;
      while (i < length) {
        if (text[i] === '"') {
          if (text[i + 1] === '"') {
            field += '"';
            i += 2;
            continue;
          }
          i += 1;
          break;
        }
        field += text[i];
        i += 1;
      }
      continue;
    }

    if (char === delimiter) {
      endField();
      i += 1;
    } else if (char === "\r" || char === "\n") {
      endRecord();
      i += char === "\r" && text[i + 1] === "\n" ? 2 : 1;
    } else {
      field += char;
      i += 1;
    }
  }

  // The last record, unless the file simply ended with a line break.
  if (field !== "" || record.length > 0) endRecord();
  return records;
}

/** Splits parsed records into a header and padded rows. */
export function toTable(records: string[][]): ParsedTable {
  const columnCount = records.reduce((max, r) => Math.max(max, r.length), 0);
  const pad = (record: string[]) =>
    record.length === columnCount
      ? record
      : [...record, ...Array<string>(columnCount - record.length).fill("")];
  const [header = [], ...rows] = records;
  return { header: pad(header), rows: rows.map(pad), columnCount };
}
