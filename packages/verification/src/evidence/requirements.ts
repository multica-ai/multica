import type { Check, Dataset, Evidence, Step } from "../contracts/resources.js";
import type { VerificationRecord } from "../contracts/index.js";
import { canonicalJson } from "../host/storage.js";
import { problem, VerificationError } from "../host/problem.js";

export function enforcePlan(
  record: VerificationRecord,
  checks: Check[],
  datasets: Dataset[],
  steps: Step[],
  evidence: Evidence[],
): void {
  const reject = (message: string): never => {
    throw new VerificationError(problem("coverageIncomplete", message));
  };
  const plan = record.plan;
  if (!plan) reject("Declare a verification plan before reporting passed");
  if (!plan) return;
  const targets = plan.checks.filter((item) => item.purpose === "target");
  if (!targets.length) reject("The plan requires a target check");
  if (
    record.kind === "bug" &&
    (!plan.dataSnapshotId ||
      !plan.checks.some((item) => item.purpose === "boundary"))
  )
    reject("Bug plans require an immutable data snapshot and a boundary check");
  const roles = record.kind === "bug" ? ["baseline", "candidate"] : ["smoke"];
  for (const role of roles) {
    const environments = record.environments.filter(
      (item) => item.role === role,
    );
    if (!environments.length) reject(`Missing ${role} environment`);
    for (const { environmentId } of environments) {
      const required = role === "baseline" ? targets : plan.checks;
      for (const declaration of required) {
        const matching = checks.filter(
          (check) =>
            check.environmentId === environmentId &&
            check.comparisonKey === declaration.key &&
            check.predicate.kind === declaration.kind &&
            check.predicate.operator === declaration.operator &&
            canonicalJson(check.predicate.expected) ===
              canonicalJson(declaration.expected) &&
            check.temporal.mode === declaration.temporal &&
            check.status === (role === "baseline" ? "failed" : "passed"),
        );
        if (!matching.length)
          reject(`Missing ${role} check: ${declaration.key}`);
        if (
          matching.some(
            (check) =>
              check.datasetId &&
              !datasets.some(
                (dataset) =>
                  dataset.datasetId === check.datasetId &&
                  (dataset.providerScript ?? "none") === plan.providerScript,
              ),
          )
        )
          reject(`Check ${declaration.key} uses another provider script`);
        const eligible = matching.filter((check) => {
          if (!check.datasetId) return !plan.dataSnapshotId;
          return datasets.some(
            (dataset) =>
              dataset.datasetId === check.datasetId &&
              dataset.environmentId === environmentId &&
              dataset.state === "ready" &&
              check.restoreVersion === dataset.restoreVersion &&
              check.dataSnapshotId === dataset.dataSnapshotId &&
              (!plan.dataSnapshotId ||
                dataset.dataSnapshotId === plan.dataSnapshotId),
          );
        });
        if (!eligible.length)
          reject(
            `Check ${declaration.key} does not use the declared snapshot and current restore version`,
          );
        const ui =
          plan.ui.required ||
          declaration.kind.startsWith("page.") ||
          eligible.some((check) =>
            steps.some(
              (step) => step.stepId === check.stepId && step.driver !== "api",
            ),
          );
        if (ui) {
          for (const colorScheme of plan.ui.colorSchemes)
            if (
              !eligible.some(
                (check) =>
                  check.stepId &&
                  evidence.some(
                    (item) =>
                      item.kind === "screenshot" &&
                      item.environmentId === environmentId &&
                      item.stepId === check.stepId &&
                      item.colorScheme === colorScheme &&
                      item.coverage === "complete",
                  ),
              )
            )
              reject(
                `Missing ${role} ${colorScheme} screenshot for ${declaration.key}`,
              );
        }
      }
    }
  }
}
