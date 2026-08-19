import { describe, expect, it } from "vitest";
import { assertReadOnlyStatement } from "./sql-guard";

describe("assertReadOnlyStatement", () => {
  it("allows a plain SELECT", () => {
    expect(() => assertReadOnlyStatement("SELECT * FROM workspace")).not.toThrow();
  });

  it("allows a SELECT with columns whose names contain a write keyword as a substring", () => {
    // "created_at" contains "create" — must not false-positive.
    expect(() =>
      assertReadOnlyStatement("SELECT w.id, w.created_at, w.updated_at FROM workspace w"),
    ).not.toThrow();
  });

  it("allows a read-only CTE (WITH ... SELECT)", () => {
    expect(() =>
      assertReadOnlyStatement("WITH recent AS (SELECT id FROM workspace) SELECT * FROM recent"),
    ).not.toThrow();
  });

  it("rejects a bare INSERT/UPDATE/DELETE/DROP", () => {
    expect(() => assertReadOnlyStatement("INSERT INTO workspace (name) VALUES ('x')")).toThrow();
    expect(() => assertReadOnlyStatement("UPDATE workspace SET name = 'x'")).toThrow();
    expect(() => assertReadOnlyStatement("DELETE FROM workspace WHERE id = 1")).toThrow();
    expect(() => assertReadOnlyStatement("DROP TABLE workspace")).toThrow();
  });

  it("rejects a data-modifying CTE hidden behind a leading WITH/SELECT shape", () => {
    expect(() =>
      assertReadOnlyStatement(
        "WITH del AS (DELETE FROM workspace WHERE id = 1 RETURNING id) SELECT * FROM del",
      ),
    ).toThrow();
  });

  it("rejects multiple statements in one call", () => {
    expect(() =>
      assertReadOnlyStatement("SELECT * FROM workspace; DROP TABLE workspace;"),
    ).toThrow();
  });
});
