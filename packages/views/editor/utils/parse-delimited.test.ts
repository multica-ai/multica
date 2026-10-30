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
  it("picks whichever candidate splits the first line most", () => {
    expect(sniffDelimiter("a,b,c\n1,2,3")).toBe(",");
    expect(sniffDelimiter("a;b;c\n1,5;2,5;3")).toBe(";");
    expect(sniffDelimiter("a\tb\tc")).toBe("\t");
  });

  it("defaults to a comma for a single column", () => {
    expect(sniffDelimiter("only\n1\n2")).toBe(",");
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
