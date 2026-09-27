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
 * record: whichever candidate splits it most wins, a comma on ties.
 */
export function sniffDelimiter(text: string): CellDelimiter {
  let end = text.search(/\r|\n/);
  if (end < 0) end = text.length;
  const firstLine = text.slice(0, end);
  let best: CellDelimiter = ",";
  let bestCount = 0;
  for (const candidate of CANDIDATES) {
    const count = firstLine.split(candidate).length - 1;
    if (count > bestCount) {
      best = candidate;
      bestCount = count;
    }
  }
  return best;
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
