import {
  ChartGantt,
  Columns3,
  List,
  Table2,
  Waves,
  Network as Waypoints,
} from "@multica/ui/icons";
import type { ViewMode } from "@multica/core/issues/surface/view-mode";
import { useT } from "../../i18n";

export const VIEW_MODE_META = {
  board: {
    icon: Columns3,
    namespace: "issues",
    label: "board",
    tooltip: "tooltip_board",
  },
  list: {
    icon: List,
    namespace: "issues",
    label: "list",
    tooltip: "tooltip_list",
  },
  table: {
    icon: Table2,
    namespace: "issues",
    label: "table",
    tooltip: "tooltip_table",
  },
  swimlane: {
    icon: Waves,
    namespace: "issues",
    label: "swimlane",
    tooltip: "tooltip_swimlane",
  },
  gantt: {
    icon: ChartGantt,
    namespace: "issues",
    label: "gantt",
    tooltip: "tooltip_gantt",
  },
  dag: {
    icon: Waypoints,
    namespace: "dag",
    label: "dag",
    tooltip: "tooltip_dag",
  },
} as const;

export function useViewModeLabels() {
  const { t } = useT("issues");
  const { t: tDag } = useT("dag");
  return {
    labelOf(mode: ViewMode) {
      const meta = VIEW_MODE_META[mode];
      return meta.namespace === "dag"
        ? tDag(($) => $.view[meta.label])
        : t(($) => $.view[meta.label]);
    },
    tooltipOf(mode: ViewMode) {
      const meta = VIEW_MODE_META[mode];
      return meta.namespace === "dag"
        ? tDag(($) => $.view[meta.tooltip])
        : t(($) => $.view[meta.tooltip]);
    },
  };
}
