// @vitest-environment node
import { describe, expect, it } from "vitest";
import { parseDelimited, sniffDelimiter, toTable } from "./parse-delimited";

describe("parseDelimited", () => {
  it("splits records and fields", () => {
    expect(parseDelimited("a,b\n1,2\n", ",")).toEqual([
      ["a", "b"],
      ["1", "2"],
    ]);
  });

  it("keeps delimiters, quotes and line breaks inside quoted fields", () => {
    const text = 'name,note\n"Doe, Jane","said ""hi""\nthen left"\n';
    expect(parseDelimited(text, ",")).toEqual([
      ["name", "note"],
      ["Doe, Jane", 'said "hi"\nthen left'],
    ]);
  });

  it("accepts CRLF and CR line endings and a missing final newline", () => {
    expect(parseDelimited("a,b\r\n1,2\r3,4", ",")).toEqual([
      ["a", "b"],
      ["1", "2"],
      ["3", "4"],
    ]);
  });

  it("strips a UTF-8 byte order mark", () => {
    expect(parseDelimited("﻿id,name\n1,x", ",")[0]).toEqual(["id", "name"]);
  });

  it("keeps empty fields but skips blank lines", () => {
    expect(parseDelimited("a,,c\n\n,,\n", ",")).toEqual([
      ["a", "", "c"],
      ["", "", ""],
    ]);
  });

  it("treats a quote that does not open a field as text", () => {
    expect(parseDelimited('5" screen,ok\n"x"y,z', ",")).toEqual([
      ['5" screen', "ok"],
      ["xy", "z"],
    ]);
  });

  it("splits TSV on tabs only", () => {
    expect(parseDelimited("a\tb,c\n1\t2", "\t")).toEqual([
      ["a", "b,c"],
      ["1", "2"],
    ]);
  });

  it("returns nothing for an empty file", () => {
    expect(parseDelimited("", ",")).toEqual([]);
  });
});

describe("sniffDelimiter", () => {
  it("picks whichever candidate splits the first record most", () => {
    expect(sniffDelimiter("a,b,c\n1,2,3")).toBe(",");
    expect(sniffDelimiter("a;b;c\n1,5;2,5;3")).toBe(";");
    expect(sniffDelimiter("a\tb\tc")).toBe("\t");
  });

  it("defaults to a comma for a single column", () => {
    expect(sniffDelimiter("only\n1\n2")).toBe(",");
  });

  it.each([",", ";", "\t"] as const)(
    "ignores other delimiters in quoted fields when detecting %j",
    (delimiter) => {
      const otherDelimiters = [",", ";", "\t"].filter((value) => value !== delimiter).join("");
      const title = `Priority${otherDelimiters.repeat(3)}impact`;
      const text = `id${delimiter}"${title}"${delimiter}Owner\n1${delimiter}high${delimiter}Ada`;
      expect(sniffDelimiter(text)).toBe(delimiter);
      expect(parseDelimited(text, sniffDelimiter(text))).toEqual([
        ["id", title, "Owner"],
        ["1", "high", "Ada"],
      ]);
    },
  );

  it.each(["\n", "\r\n", "\r"])(
    "reads a quoted header across %j line breaks",
    (newline) => {
      const text = `"Priority;${newline}severity; impact",Owner${newline}high,Ada`;
      expect(sniffDelimiter(text)).toBe(",");
      expect(parseDelimited(text, sniffDelimiter(text))[0]).toEqual([
        `Priority;${newline}severity; impact`,
        "Owner",
      ]);
    },
  );

  it("keeps escaped quotes inside a quoted field", () => {
    expect(sniffDelimiter('id;"said ""hi, there, friend""";Owner\n1;ok;Ada')).toBe(";");
  });

  it("recognizes a quoted first field after a byte order mark", () => {
    expect(sniffDelimiter('\ufeff"Priority; severity; impact",Owner\nhigh,Ada')).toBe(",");
  });

  it("treats quotes within an unquoted field as text", () => {
    expect(sniffDelimiter('5" screen;Owner\nlarge;Ada')).toBe(";");
  });

  it("handles empty quoted fields and trailing empty fields", () => {
    expect(sniffDelimiter('"";"";\n1;2;3')).toBe(";");
  });

  it("does not count delimiters inside a single quoted column", () => {
    expect(sniffDelimiter('"a;b;c\nmore; text"\nvalue')).toBe(",");
  });

  it.each(["\n", "\r\n", "\r"])(
    "stops at the first unquoted %j record ending",
    (newline) => {
      expect(sniffDelimiter(`a;b${newline}${"1,2,3,4\n".repeat(10_000)}`)).toBe(";");
    },
  );

  it("handles a missing final newline and an unterminated quote", () => {
    expect(sniffDelimiter('id;"a,b,c"')).toBe(";");
    expect(sniffDelimiter('id;"a,b,c')).toBe(";");
  });

  it("keeps candidate precedence on ties and defaults for empty input", () => {
    expect(sniffDelimiter("a,b;c\td")).toBe(",");
    expect(sniffDelimiter("a;b\tc")).toBe(";");
    expect(sniffDelimiter("")).toBe(",");
  });
});

describe("toTable", () => {
  it("pads ragged records to the widest one", () => {
    expect(toTable([["a", "b"], ["1"], ["1", "2", "3"]])).toEqual({
      header: ["a", "b", ""],
      rows: [
        ["1", "", ""],
        ["1", "2", "3"],
      ],
      columnCount: 3,
    });
  });

  it("handles no records at all", () => {
    expect(toTable([])).toEqual({ header: [], rows: [], columnCount: 0 });
  });
});
