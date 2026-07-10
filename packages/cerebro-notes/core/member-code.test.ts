import { describe, expect, it } from "vitest";
import { memberCode } from "./member-code";

describe("memberCode", () => {
  it("uses first two letters of first name + first letter of last name", () => {
    expect(memberCode("Jesper Hvejsel")).toBe("JEH");
    expect(memberCode("Morten Krøjer Persson")).toBe("MOP");
  });

  it("takes the first three letters of a single-word name", () => {
    expect(memberCode("Sabine")).toBe("SAB");
  });

  it("survives extra whitespace", () => {
    expect(memberCode("  Jesper   Hvejsel  ")).toBe("JEH");
  });

  it("uppercases Danish letters", () => {
    expect(memberCode("Åse Østergaard")).toBe("ÅSØ");
  });

  it("returns empty string for blank names", () => {
    expect(memberCode("")).toBe("");
    expect(memberCode("   ")).toBe("");
  });

  it("handles very short names without crashing", () => {
    expect(memberCode("Bo")).toBe("BO");
    expect(memberCode("Bo Li")).toBe("BOL");
  });
});
