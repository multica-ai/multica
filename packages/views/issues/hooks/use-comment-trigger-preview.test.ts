import { describe, expect, it } from "vitest";
import { commentTriggerPreviewSignature } from "./use-comment-trigger-preview";

describe("commentTriggerPreviewSignature", () => {
  it("ignores ordinary text changes", () => {
    expect(commentTriggerPreviewSignature("hello")).toBe(
      commentTriggerPreviewSignature("hello with more ordinary text"),
    );
  });

  it("changes when routing mentions change", () => {
    const agentA = "00000000-0000-0000-0000-000000000001";
    const agentB = "00000000-0000-0000-0000-000000000002";

    expect(commentTriggerPreviewSignature(`[@A](mention://agent/${agentA})`)).not.toBe(
      commentTriggerPreviewSignature(`[@A](mention://agent/${agentA}) [@B](mention://agent/${agentB})`),
    );
  });

  it("tracks @all but ignores issue cross-references", () => {
    const issueID = "00000000-0000-0000-0000-000000000003";

    expect(commentTriggerPreviewSignature(`See [MUL-1](mention://issue/${issueID})`)).toBe(
      commentTriggerPreviewSignature("plain text"),
    );
    expect(commentTriggerPreviewSignature("[@all](mention://all/all)")).not.toBe(
      commentTriggerPreviewSignature("plain text"),
    );
  });
});
