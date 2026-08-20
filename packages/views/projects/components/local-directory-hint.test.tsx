import { describe, expect, it } from "vitest";
import { render } from "@testing-library/react";

import { LocalDirectoryHint } from "./local-directory-hint";

describe("LocalDirectoryHint", () => {
  it.each([null, undefined, "proj-1"])(
    "renders no machine-local hint in the browser for project %s",
    (projectId) => {
      const { container } = render(
        <LocalDirectoryHint projectId={projectId} />,
      );

      expect(container.firstChild).toBeNull();
    },
  );
});
