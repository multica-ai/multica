import { Component, createRef, type JSX } from "react";
import { AtomIcon } from "../../../../../app/renderer/components/icons/AtomIcon";
import { translate } from "../../../../../i18n/index";
import { AgentPathTailLabel } from "../AgentPathTailLabel";
import { loadMonaco, type MonacoModule } from "./loadMonaco";
import { observer } from "mobx-react-lite";
import { observer as observeClass } from "mobx-react";
export const AgentMonacoDiffViewer = observer(AgentMonacoDiffViewerRender);

interface MonacoDiffResources {
  monaco: MonacoModule;
  editor: import("monaco-editor").editor.IStandaloneDiffEditor;
  originalModel: import("monaco-editor").editor.ITextModel;
  modifiedModel: import("monaco-editor").editor.ITextModel;
}

interface MonacoDiffContent {
  oldValue: string;
  newValue: string;
  language: string;
}

type MonacoDiffEditorSurfaceProps = MonacoDiffContent;

interface MonacoDiffEditorSurfaceState {
  loadError: Error | null;
  loading: boolean;
}

function AgentMonacoDiffViewerRender({
  path,
  oldValue,
  newValue,
  flat = false,
  showHeader = true
}: {
  path?: string | null;
  oldValue: string;
  newValue: string;
  flat?: boolean;
  showHeader?: boolean;
}): JSX.Element {
  "use memo";

  return (
    <div
      className={`overflow-hidden rounded-[8px] border border-[var(--line-2)] bg-[var(--background-panel)] ${
        flat ? "workspace-agents-status-panel__detail-tool-monaco--flat" : ""
      }`}
    >
      {showHeader ? (
        <div
          className="border-b border-[var(--line-2)] bg-[var(--transparency-block)] px-3 py-1.5 text-[11px] text-[var(--text-secondary)]"
          data-agent-diff-header="true"
        >
          <AgentPathTailLabel
            path={path}
            fallback="Diff"
            className="font-[var(--tsh-font-mono)]"
          />
        </div>
      ) : null}
      <div className="h-[220px] bg-[var(--background-panel)]">
        <MonacoDiffEditorSurface
          oldValue={oldValue}
          newValue={newValue}
          language={languageForPath(path)}
        />
      </div>
    </div>
  );
}

// Monaco's diff editor and its two text models form one imperative resource
// lifetime. A class component makes the required mount/update/unmount ordering
// explicit without coordinating multiple React effects.
class MonacoDiffEditorSurface extends Component<
  MonacoDiffEditorSurfaceProps,
  MonacoDiffEditorSurfaceState
> {
  private readonly containerRef = createRef<HTMLDivElement>();
  private resources: MonacoDiffResources | null = null;
  private loadGeneration = 0;

  state: MonacoDiffEditorSurfaceState = {
    loadError: null,
    loading: true
  };

  componentDidMount(): void {
    const loadGeneration = ++this.loadGeneration;
    void loadMonaco()
      .then((monaco) => {
        const container = this.containerRef.current;
        if (loadGeneration !== this.loadGeneration || !container) {
          return;
        }
        this.resources = createMonacoDiffResources(
          monaco,
          container,
          this.props
        );
        this.setState({ loading: false });
      })
      .catch((error: unknown) => {
        if (loadGeneration === this.loadGeneration) {
          this.setState({
            loadError: error instanceof Error ? error : new Error(String(error))
          });
        }
      });
  }

  componentDidUpdate(): void {
    if (this.resources) {
      updateMonacoDiffResources(this.resources, this.props);
    }
  }

  componentWillUnmount(): void {
    this.loadGeneration += 1;
    const resources = this.resources;
    this.resources = null;
    if (resources) {
      disposeMonacoDiffResources(resources);
    }
  }

  render(): JSX.Element {
    if (this.state.loadError) {
      throw this.state.loadError;
    }

    return (
      <div className="relative h-full w-full">
        <div ref={this.containerRef} className="h-full w-full" />
        {this.state.loading ? (
          <div className="absolute inset-0 flex items-start gap-1.5 px-3 py-2.5 text-[11px] text-[var(--text-secondary)]">
            <AtomIcon
              size={14}
              active
              aria-hidden="true"
              className="shrink-0"
            />
            <span>{translate("agentHost.agentTool.details.loadingDiff")}</span>
          </div>
        ) : null}
      </div>
    );
  }
}
observeClass(MonacoDiffEditorSurface);

function createMonacoDiffResources(
  monaco: MonacoModule,
  container: HTMLDivElement,
  content: MonacoDiffContent
): MonacoDiffResources {
  const originalModel = monaco.editor.createModel(
    content.oldValue,
    content.language
  );
  const modifiedModel = monaco.editor.createModel(
    content.newValue,
    content.language
  );
  let editor: import("monaco-editor").editor.IStandaloneDiffEditor | null =
    null;

  try {
    monaco.editor.setTheme("light");
    editor = monaco.editor.createDiffEditor(container, {
      readOnly: true,
      renderSideBySide: true,
      minimap: { enabled: false },
      scrollBeyondLastLine: false,
      wordWrap: "on",
      automaticLayout: true
    });
    editor.setModel({
      original: originalModel,
      modified: modifiedModel
    });
    return { monaco, editor, originalModel, modifiedModel };
  } catch (error) {
    if (editor) {
      try {
        editor.setModel(null);
      } finally {
        try {
          editor.dispose();
        } finally {
          disposeMonacoModels(originalModel, modifiedModel);
        }
      }
    } else {
      disposeMonacoModels(originalModel, modifiedModel);
    }
    throw error;
  }
}

function updateMonacoDiffResources(
  resources: MonacoDiffResources,
  content: MonacoDiffContent
): void {
  if (resources.originalModel.getValue() !== content.oldValue) {
    resources.originalModel.setValue(content.oldValue);
  }
  if (resources.modifiedModel.getValue() !== content.newValue) {
    resources.modifiedModel.setValue(content.newValue);
  }

  const currentLanguage = resources.originalModel.getLanguageId();
  if (currentLanguage !== content.language) {
    resources.monaco.editor.setModelLanguage(
      resources.originalModel,
      content.language
    );
    resources.monaco.editor.setModelLanguage(
      resources.modifiedModel,
      content.language
    );
  }
}

function disposeMonacoDiffResources(resources: MonacoDiffResources): void {
  try {
    resources.editor.setModel(null);
  } finally {
    try {
      resources.editor.dispose();
    } finally {
      disposeMonacoModels(resources.originalModel, resources.modifiedModel);
    }
  }
}

function disposeMonacoModels(
  originalModel: import("monaco-editor").editor.ITextModel,
  modifiedModel: import("monaco-editor").editor.ITextModel
): void {
  try {
    originalModel.dispose();
  } finally {
    modifiedModel.dispose();
  }
}

function languageForPath(path: string | null | undefined): string {
  const extension = path?.split(".").pop()?.toLowerCase();
  switch (extension) {
    case "ts":
    case "tsx":
      return "typescript";
    case "js":
    case "jsx":
      return "javascript";
    case "go":
      return "go";
    case "md":
      return "markdown";
    case "json":
      return "json";
    default:
      return "plaintext";
  }
}
