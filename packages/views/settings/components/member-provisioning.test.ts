import { describe, expect, it } from "vitest";
import { parseProvisioningEmails } from "./member-provisioning";

describe("parseProvisioningEmails", () => {
  it("normalizes delimiters, casing, duplicates, and invalid entries", () => {
    expect(
      parseProvisioningEmails(
        " Alice@Company.com\nbob@company.com;ALICE@company.com,not-an-email ",
      ),
    ).toEqual({
      emails: ["alice@company.com", "bob@company.com"],
      duplicates: ["alice@company.com"],
      invalid: ["not-an-email"],
      total: 4,
    });
  });

  it("ignores empty delimiters", () => {
    expect(parseProvisioningEmails("\n, ; \t")).toEqual({
      emails: [],
      duplicates: [],
      invalid: [],
      total: 0,
    });
  });
});
