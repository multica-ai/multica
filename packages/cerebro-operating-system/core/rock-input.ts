import type { Rock, RockInput } from "./types";

/** Build a full RockInput from an existing Rock so a single field can be edited inline without losing the rest. */
export function rockToInput(rock: Rock, overrides: Partial<RockInput> = {}): RockInput {
  return {
    title: rock.title,
    description: rock.description ?? "",
    owner_type: rock.owner_type === "member" || rock.owner_type === "agent" ? rock.owner_type : undefined,
    owner_id: rock.owner_id || undefined,
    period_id: rock.period_id,
    goal_type_id: rock.goal_type_id || undefined,
    confidence: rock.confidence,
    reported_health: rock.reported_health === "unknown" ? "unset" : rock.reported_health,
    project_ids: rock.projects.map((project) => project.id),
    issue_ids: rock.issues.filter((issue) => !issue.project_id).map((issue) => issue.id),
    strategy_item_id: rock.strategy_item_id || undefined,
    ...overrides,
  };
}
