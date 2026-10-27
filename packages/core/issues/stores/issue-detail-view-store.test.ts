// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from "vitest";
import { useIssueDetailViewStore } from "./issue-detail-view-store";

describe("issue detail view store", () => {
  beforeEach(() => {
    useIssueDetailViewStore.setState({ activityOrder: "oldest" });
  });

  it("defaults to oldest activity first", () => {
    expect(useIssueDetailViewStore.getState().activityOrder).toBe("oldest");
  });

  it("updates the activity order preference", () => {
    useIssueDetailViewStore.getState().setActivityOrder("newest");
    expect(useIssueDetailViewStore.getState().activityOrder).toBe("newest");
  });
});
