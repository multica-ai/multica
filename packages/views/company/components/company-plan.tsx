"use client";

import { IssueSurface } from "../../issues/surface/issue-surface";
import { CompanyHeader } from "./company-header";
import { CompanyView } from "./company-view";

/**
 * The Issues board drawn as the pixtuoid company. It runs on the same issue
 * surface, so filters and peek work as they do on the board. It keeps its own
 * display state and stays out of the saved views of /issues.
 */
export function CompanyFloorPlan() {
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <IssueSurface
        scope={{ type: "workspace" }}
        modes={["board"]}
        surfaceKey="company"
        savedViews={false}
        batchToolbar="never"
        renderHeader={({ controller }) => <CompanyHeader controller={controller} />}
        renderBoard={(props) => <CompanyView {...props} />}
      />
    </div>
  );
}
