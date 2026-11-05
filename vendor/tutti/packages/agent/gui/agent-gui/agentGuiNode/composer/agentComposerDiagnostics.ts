import type {
  AgentActivityRuntimeDiagnosticInput,
  ConversationServices
} from "../../../conversationServices";

export function reportAgentComposerDiagnostic(
  runtime: ConversationServices | null,
  input: AgentActivityRuntimeDiagnosticInput
): void {
  const reportDiagnostic = runtime?.diagnostics?.reportDiagnostic;
  try {
    if (reportDiagnostic && runtime) {
      void Promise.resolve(reportDiagnostic.call(runtime, input)).catch(
        (error: unknown) => {
          reportAgentComposerDiagnosticFailure(input, error);
        }
      );
    }
    if (!runtime || !agentComposerDevConsoleDiagnosticSinkEnabled(runtime)) {
      return;
    }
    const level = input.level ?? "info";
    const consoleMethod =
      level === "error"
        ? console.error
        : level === "warn"
          ? console.warn
          : level === "debug"
            ? console.debug
            : console.info;
    consoleMethod.call(console, "[agent-gui]", JSON.stringify(input));
  } catch (error) {
    reportAgentComposerDiagnosticFailure(input, error);
  }
}

function reportAgentComposerDiagnosticFailure(
  input: AgentActivityRuntimeDiagnosticInput,
  error: unknown
): void {
  console.warn(
    "[agent-gui]",
    JSON.stringify({
      event: "agent.gui.composer.diagnostic_failed",
      level: "warn",
      source: "agent-gui",
      workspaceId: input.workspaceId ?? null,
      details: {
        diagnosticEvent: input.event,
        error: error instanceof Error ? error.message : String(error)
      }
    })
  );
}

function agentComposerDevConsoleDiagnosticSinkEnabled(
  runtime: ConversationServices
): boolean {
  return (
    runtime.diagnostics?.devDiagnosticConsoleSink !== false &&
    typeof process !== "undefined" &&
    (process.env.NODE_ENV === "development" ||
      process.env.AGENT_GUI_DEV_DIAGNOSTIC_CONSOLE === "1")
  );
}
