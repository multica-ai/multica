"use client";

import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { AccessDiagnostics } from "@multica/cerebro-ui";
import type { AccessDiagnostic } from "@multica/core/types";

export function ConnectionAccessDiagnostics({
  diagnostics,
}: {
  diagnostics: AccessDiagnostic[];
}) {
  const enabled = useFeatureFlag("cerebro_access_diagnostics");
  if (!enabled) return null;
  return (
    <AccessDiagnostics
      diagnostics={diagnostics}
      emptyMessage="No discovery diagnostics were returned."
      className="mb-3"
    />
  );
}
