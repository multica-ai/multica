import { describe, expect, it } from "vitest";

import { acceptScopeSuggestion, pendingScopeSuggestions } from "./scope-suggestions";

const suggestion = {
  tool: "query_run",
  arg: "data_source_id",
  options_source_tool: "data_sources_list",
  group_by: "folder",
  tag_field: "tags",
  label: "Data source",
};

describe("scope suggestions", () => {
  it("keeps only new tool/argument suggestions", () => {
    expect(pendingScopeSuggestions([suggestion], [suggestion])).toEqual([]);
    expect(pendingScopeSuggestions([], [suggestion])).toEqual([suggestion]);
  });

  it("adds an accepted suggestion once", () => {
    expect(acceptScopeSuggestion([], suggestion)).toEqual([suggestion]);
    expect(acceptScopeSuggestion([suggestion], suggestion)).toEqual([suggestion]);
  });
});
