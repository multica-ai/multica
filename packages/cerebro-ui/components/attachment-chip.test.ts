import { describe, expect, it } from "vitest";
import { attachmentDocType } from "./attachment-chip";

describe("attachmentDocType", () => {
  it("maps known document extensions to their palette type", () => {
    expect(attachmentDocType("report.pdf")).toBe("pdf");
    expect(attachmentDocType("budget.xlsx")).toBe("excel");
    expect(attachmentDocType("data.csv")).toBe("excel");
    expect(attachmentDocType("brief.docx")).toBe("word");
    expect(attachmentDocType("deck.pptx")).toBe("powerpoint");
    expect(attachmentDocType("bundle.zip")).toBe("archive");
    expect(attachmentDocType("main.ts")).toBe("code");
    expect(attachmentDocType("query.sql")).toBe("code");
  });

  it("is case-insensitive and handles paths", () => {
    expect(attachmentDocType("REPORT.PDF")).toBe("pdf");
    expect(attachmentDocType("/tmp/sub/Sheet.XLSX")).toBe("excel");
  });

  it("falls back to 'other' for unknown or extension-less names", () => {
    expect(attachmentDocType("mystery.bin")).toBe("other");
    expect(attachmentDocType("Makefile")).toBe("other");
    expect(attachmentDocType("noext")).toBe("other");
    expect(attachmentDocType(".gitignore")).toBe("other");
  });
});
