import { clearDevErrors, clearDevRequests } from "@multica/core/diagnostics";
import type { DevFeature } from "./actions";

export const queryCacheDevFeature: DevFeature = {
  id: "query-cache",
  group: "Data",
  title: "Query cache",
  actions: [
    {
      id: "invalidate",
      title: "Invalidate all queries",
      description: "Marks everything stale; what is on screen refetches now",
      run: ({ queryClient }) => void queryClient.invalidateQueries(),
    },
    {
      id: "reset",
      title: "Reset all queries",
      description: "Drops cached data, so screens show their loading state",
      run: ({ queryClient }) => void queryClient.resetQueries(),
    },
  ],
};

export const errorsDevFeature: DevFeature = {
  id: "errors",
  group: "Errors",
  title: "Error capture",
  actions: [
    {
      id: "console-error",
      title: "Log a console error",
      run: () => console.error(new Error("Dev toolbar test error")),
    },
    {
      id: "throw",
      title: "Throw an uncaught error",
      description: "Thrown outside React, as a failed event handler would",
      run: () => {
        setTimeout(() => {
          throw new Error("Dev toolbar uncaught test error");
        });
      },
    },
    {
      id: "reject",
      title: "Reject a promise",
      description: "Leaves the rejection unhandled",
      run: () => void Promise.reject(new Error("Dev toolbar unhandled rejection")),
    },
    { id: "clear-errors", title: "Clear recorded errors", run: clearDevErrors },
    { id: "clear-requests", title: "Clear recorded requests", run: clearDevRequests },
  ],
};
