import { Suspense } from "react";
import { act, render } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { getRouteRegex } from "next/dist/shared/lib/router/utils/route-regex";
import { getRouteMatcher } from "next/dist/shared/lib/router/utils/route-matcher";
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

// Next's own matcher for this route, so the params below are the ones the
// page really receives.
const matchRoute = getRouteMatcher(
  getRouteRegex("/[workspaceSlug]/issues/[id]/deliverables/[name]"),
);

async function renderLink(filename: string) {
  const href = paths.workspace("acme").issueDeliverable("MUL-1", filename);
  const params = matchRoute(href);
  if (!params) throw new Error(`route did not match ${href}`);
  await act(async () => {
    render(
      <Suspense fallback={null}>
        <IssueDeliverablePage
          params={Promise.resolve(params as { id: string; name: string })}
        />
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
    // Literal percent escapes in the file name: Next has already decoded the
    // segment once, and a second decode would name a different file.
    "report%20final.html",
    "report%25final.html",
    "100%.html",
  ])("opens the deliverable the link was built for: %s", async (filename) => {
    const props = await renderLink(filename);
    expect(props).toEqual({ routeId: "MUL-1", deliverable: filename });
  });
});
