import "@fontsource-variable/geist-mono";
import "@fontsource-variable/inter";
import "@fontsource-variable/inter/wght-italic.css";
import "@fontsource-variable/source-serif-4";
import "@fontsource-variable/source-serif-4/wght-italic.css";
import { AgentGUIStartupShell } from "@tutti-os/agent-gui/startup-shell";
import { Button } from "@tutti-os/ui-system/components";
import {
Component,
Profiler,
useEffect,
useState,
type ErrorInfo,
type ReactNode
} from "react";
import {
AGENT_GUI_ROOT_CLASS,
applyAgentGuiAppearance,
syncAgentGuiDocument
} from "./agent-gui-appearance";
import "./agent-gui.generated.css";
import { AgentGuiApplication } from "./application";
import type {
AgentGuiHostServices,
AgentGuiInitSnapshot,
LocalConversationSource
} from "./protocol";
import { createMulticaAgentGuiRuntime } from "./runtime";

function ErrorView({
  error,
  chinese,
  onRetry
}: {
  error: Error;
  chinese: boolean;
  onRetry(): void;
}) {
  return (
    <div
      role="alert"
      className="flex h-full flex-col items-center justify-center gap-3 p-6 text-sm text-[var(--text-primary)]"
    >
      <p>{chinese ? "无法加载智能体界面" : "Could not load Agent GUI"}</p>
      <details className="max-w-full overflow-auto text-[var(--text-secondary)]">
        <summary>{chinese ? "错误详情" : "Error details"}</summary>
        <pre className="whitespace-pre-wrap">{error.message}</pre>
      </details>
      <Button onClick={onRetry}>{chinese ? "重试" : "Retry"}</Button>
    </div>
  );
}

class AgentGuiErrorBoundary extends Component<
  { chinese: boolean; onRetry(): void; children: ReactNode },
  { error: Error | null }
> {
  state = { error: null as Error | null };
  static getDerivedStateFromError(error: Error) {
    return { error };
  }
  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error("Agent GUI rendering failed", error, info.componentStack);
  }
  render() {
    return this.state.error ? (
      <ErrorView
        error={this.state.error}
        chinese={this.props.chinese}
        onRetry={this.props.onRetry}
      />
    ) : (
      this.props.children
    );
  }
}

function Rendered({
  onRendered,
  children
}: {
  onRendered?: () => void;
  children: ReactNode;
}) {
  useEffect(() => {
    onRendered?.();
  }, [onRendered]);
  return children;
}

type Handle = ReturnType<typeof createMulticaAgentGuiRuntime>;

export interface AgentGuiMountProps {
  client: AgentGuiHostServices;
  initial: AgentGuiInitSnapshot;
  localConversations: LocalConversationSource;
  onRendered?: () => void;
}

/** One GUI instance. Its runtime lives exactly as long as this component. */
export default function AgentGuiMount({
  client,
  initial,
  localConversations,
  onRendered
}: AgentGuiMountProps) {
  const [root, setRoot] = useState<HTMLDivElement | null>(null);
  const [state, setState] = useState<{
    handle: Handle | null;
    error: Error | null;
  }>({ handle: null, error: null });
  const [attempt, setAttempt] = useState(0);
  const chinese = initial.locale === "zh-CN";

  useEffect(() => {
    let current = true;
    const handle = createMulticaAgentGuiRuntime(
      client,
      initial,
      localConversations
    );
    setState({ handle: null, error: null });
    handle.initialize().then(
      () => {
        if (current) setState({ handle, error: null });
      },
      (error: unknown) => {
        if (current)
          setState({
            handle: null,
            error: error instanceof Error ? error : new Error(String(error))
          });
      }
    );
    return () => {
      current = false;
      handle.dispose();
    };
    // `initial` only seeds a runtime; later snapshots arrive as init events.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [client, attempt]);

  useEffect(() => {
    syncAgentGuiDocument(initial);
  }, [initial]);

  const retry = () => setAttempt((value) => value + 1);
  return (
    <div
      ref={(element) => {
        setRoot(element);
        if (element && !element.dataset.theme)
          applyAgentGuiAppearance(element, initial);
      }}
      className={AGENT_GUI_ROOT_CLASS}
      data-testid="agent-gui-root"
    >
      {state.error ? (
        <Rendered onRendered={onRendered}>
          <ErrorView error={state.error} chinese={chinese} onRetry={retry} />
        </Rendered>
      ) : state.handle && root ? (
        <Rendered onRendered={onRendered}>
          <AgentGuiErrorBoundary
            key={attempt}
            chinese={chinese}
            onRetry={retry}
          >
            <Profiler
              id="agent-gui"
              onRender={(_id, _phase, actualDuration) =>
                state.handle?.rendered(actualDuration)
              }
            >
              <AgentGuiApplication
                client={client}
                initial={initial}
                handle={state.handle}
                root={root}
              />
            </Profiler>
          </AgentGuiErrorBoundary>
        </Rendered>
      ) : (
        <AgentGUIStartupShell loadingLabel="Loading Agent GUI" />
      )}
    </div>
  );
}
