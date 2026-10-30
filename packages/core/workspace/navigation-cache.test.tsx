// @vitest-environment jsdom
import { StrictMode, useEffect } from "react";
import { render, cleanup } from "@testing-library/react";
import { dehydrate, hydrate, QueryClient, useQueryClient } from "@tanstack/react-query";
import { afterEach, describe, expect, it } from "vitest";
import { QueryProvider } from "../provider";
import { bindWorkspaceSlugLookup, createQueryClient } from "../query-client";
import { workspaceSlugForPath } from "../paths/workspace-slugs";
import { workspaceKeys } from "./queries";

let unbind: () => void = () => {};

afterEach(() => {
  cleanup();
  unbind();
});

describe("workspace slug lookup binding", () => {
  it("reads hydration and cache changes, and forgets membership on account cleanup", () => {
    const source = new QueryClient();
    source.setQueryData(workspaceKeys.list(), [{ slug: "dingtalk" }]);
    const client = createQueryClient();
    unbind = bindWorkspaceSlugLookup(client);
    expect(workspaceSlugForPath("/dingtalk/issues")).toBeNull();
    hydrate(client, dehydrate(source));
    expect(workspaceSlugForPath("/dingtalk/issues")).toBe("dingtalk");
    client.setQueryData(workspaceKeys.list(), [{ slug: "issues" }]);
    expect(workspaceSlugForPath("/dingtalk/issues")).toBeNull();
    expect(workspaceSlugForPath("/issues/issues")).toBe("issues");
    client.clear();
    expect(workspaceSlugForPath("/issues/issues")).toBeNull();
    source.clear();
  });

  it("does not bind a client just by constructing it", () => {
    const client = createQueryClient();
    client.setQueryData(workspaceKeys.list(), [{ slug: "dingtalk" }]);
    expect(workspaceSlugForPath("/dingtalk/issues")).toBeNull();
  });

  it("a stale unbind does not clear a newer binding", () => {
    const first = createQueryClient();
    const second = createQueryClient();
    second.setQueryData(workspaceKeys.list(), [{ slug: "dingtalk" }]);
    const unbindFirst = bindWorkspaceSlugLookup(first);
    unbind = bindWorkspaceSlugLookup(second);
    unbindFirst();
    expect(workspaceSlugForPath("/dingtalk/issues")).toBe("dingtalk");
  });

  it.each([false, true])("binds the mounted provider's client (StrictMode=%s)", (strict) => {
    let mounted: QueryClient | undefined;
    function Probe() {
      const client = useQueryClient();
      useEffect(() => {
        mounted = client;
        client.setQueryData(workspaceKeys.list(), [{ slug: "dingtalk" }]);
      }, [client]);
      return null;
    }
    const tree = (
      <QueryProvider>
        <Probe />
      </QueryProvider>
    );
    const { unmount } = render(strict ? <StrictMode>{tree}</StrictMode> : tree);
    expect(workspaceSlugForPath("/dingtalk/issues")).toBe("dingtalk");
    unmount();
    expect(workspaceSlugForPath("/dingtalk/issues")).toBeNull();
    mounted?.clear();
  });
});
