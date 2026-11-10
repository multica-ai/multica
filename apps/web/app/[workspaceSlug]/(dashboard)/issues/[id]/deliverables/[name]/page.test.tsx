import { Suspense } from "react";
import { act, render } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { getRouteRegex } from "next/dist/shared/lib/router/utils/route-regex";
import { getRouteMatcher } from "next/dist/shared/lib/router/utils/route-matcher";
import { getDynamicParam } from "next/dist/shared/lib/router/utils/get-dynamic-param";
import { paths } from "@multica/core/paths";

const routeProps = vi.hoisted(() => ({
  last: null as { routeId: string; deliverable?: string } | null,
}));

vi.mock("@multica/views/issues/components", () => ({
  IssueDetailRoute: (props: { routeId: string; deliverable?: string }) => {
    routeProps.last = props;
    return null;
  },
}));

import IssueDeliverablePage from "./page";

// Next's own matcher for this route, then the step the App Router runs on
// each matched value before a page sees it, so the params below are the ones
// the page really receives: the matcher decodes the segment and
// `getDynamicParam` percent-encodes it again.
const matchRoute = getRouteMatcher(
  getRouteRegex("/[workspaceSlug]/issues/[id]/deliverables/[name]"),
);

function pageParam(matched: Parameters<typeof getDynamicParam>[0], key: string): string {
  return getDynamicParam(matched, key, "d", null, null).value as string;
}

async function renderLink(filename: string) {
  const href = paths.workspace("acme").issueDeliverable("MUL-1", filename);
  const matched = matchRoute(href);
  if (!matched) throw new Error(`route did not match ${href}`);
  const params = { id: pageParam(matched, "id"), name: pageParam(matched, "name") };
  await act(async () => {
    render(
      <Suspense fallback={null}>
        <IssueDeliverablePage params={Promise.resolve(params)} />
      </Suspense>,
    );
  });
  return routeProps.last;
}

beforeEach(() => {
  routeProps.last = null;
});

describe("deliverable link page", () => {
  it.each([
    "report.html",
    "调研 报告.html",
    "Q3 调研/报告 #2?.html",
    "レポート 最終.html",
    // Literal percent escapes in the file name: exactly one decode gives the
    // name back, a second would name a different file.
    "report%20final.html",
    "report%25final.html",
    "100%.html",
  ])("opens the deliverable the link was built for: %s", async (filename) => {
    const props = await renderLink(filename);
    expect(props).toEqual({ routeId: "MUL-1", deliverable: filename });
  });
});
