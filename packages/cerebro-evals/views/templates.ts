import { EMPTY_FORM, formFromEval, type DraftForm } from "./form";
import type { CerebroEval } from "../types";

// templates.ts holds the "Start from template" presets (screen 2) and the
// duplicate helper. A template is just a fully-formed DraftForm the create form
// can adopt, so the same buildWriteInput path validates it — a template can
// never write a shape the runner would not parse. Kept pure so each preset is
// unit-tested without a DOM.

export interface EvalTemplate {
  id: string;
  name: string;
  description: string;
  build: () => DraftForm;
}

let taskSeq = 0;
// Stable per-render ids are unnecessary here: the create form re-keys tasks
// when the template is adopted. A simple counter keeps template tasks distinct.
function task(situation: string, expected: string, critical = false): DraftForm["tasks"][number] {
  taskSeq += 1;
  return { id: `tpl-task-${taskSeq}`, situation, expected, critical };
}

function preset(partial: Partial<DraftForm> & { tasks: DraftForm["tasks"] }): DraftForm {
  return { ...EMPTY_FORM, ...partial };
}

// Five concrete starting points plus the blank fallback the create form already
// offers. Each is a draft the user tailors — target, tasks and threshold are all
// still editable after adoption.
export const EVAL_TEMPLATES: EvalTemplate[] = [
  {
    id: "customer-service-quality",
    name: "Customer-service reply quality",
    description: "Judge whether a reply is correct, on-policy and in the right tone.",
    build: () => preset({
      key: "customer-service-quality", title: "Customer-service reply quality",
      objective: "The reply answers the customer accurately, stays on policy and keeps a helpful tone.",
      targetKind: "agent", graderType: "ai_judge",
      graderRubric: "Pass only if the reply is factually correct, follows refund/shipping policy, and is polite and clear.",
      passRate: "90", requireAllCritical: true,
      tasks: [
        task("Customer asks where their delayed order is.", "Acknowledges the delay, gives the tracking status, offers a next step.", true),
        task("Customer is angry about a wrong item.", "Apologises, confirms the mistake, explains the replacement/refund path."),
      ],
    }),
  },
  {
    id: "refund-policy-adherence",
    name: "Refund-policy adherence",
    description: "Every critical policy case must pass — a wrong refund promise sinks the run.",
    build: () => preset({
      key: "refund-policy-adherence", title: "Refund-policy adherence",
      objective: "The agent never promises a refund, return label or cancellation the policy does not allow.",
      targetKind: "agent", graderType: "ai_judge",
      graderRubric: "Fail if the answer promises a refund/return label/cancellation that policy forbids, or invents an amount.",
      passRate: "100", requireAllCritical: true,
      tasks: [
        task("Customer wants to cancel an already-shipped order.", "Explains it cannot be cancelled now and describes the return process instead.", true),
        task("Customer asks for a free return label on a change-of-mind return.", "States the customer pays return shipping for change-of-mind.", true),
      ],
    }),
  },
  {
    id: "product-recommendation-relevance",
    name: "Product-recommendation relevance",
    description: "Judge whether recommended products actually match the customer's need.",
    build: () => preset({
      key: "product-recommendation-relevance", title: "Product-recommendation relevance",
      objective: "Recommended products match the stated need, budget and constraints.",
      targetKind: "agent", graderType: "ai_judge",
      graderRubric: "Pass if every recommended product fits the stated need and any budget or ingredient constraint.",
      passRate: "80", requireAllCritical: false,
      tasks: [
        task("Customer wants a magnesium supplement without additives, under 150 kr.", "Recommends an additive-free magnesium product within budget."),
      ],
    }),
  },
  {
    id: "structured-output-format",
    name: "Structured-output format",
    description: "Deterministic check that the output contains the required marker or field.",
    build: () => preset({
      key: "structured-output-format", title: "Structured-output format",
      objective: "The output is in the required machine-readable shape.",
      targetKind: "skill", graderType: "hard_rule", graderMatch: "contains",
      passRate: "100", requireAllCritical: true,
      tasks: [
        task("Ask the target to return the order status as JSON.", "\"status\":", true),
      ],
    }),
  },
  {
    id: "danish-tone-and-language",
    name: "Danish tone & language",
    description: "Judge whether customer-facing text is written in correct, plain Danish.",
    build: () => preset({
      key: "danish-tone-and-language", title: "Danish tone & language",
      objective: "Customer-facing text is written in correct, natural Danish with a plain, friendly tone.",
      targetKind: "agent", graderType: "ai_judge",
      graderRubric: "Pass if the text is in fluent Danish, free of obvious grammar errors, and keeps a plain friendly tone.",
      passRate: "90", requireAllCritical: false,
      tasks: [
        task("Reply to a customer thanking them for their patience during a delay.", "Fluent, natural Danish with a warm, plain tone."),
      ],
    }),
  },
];

// duplicateForm clones an existing eval into a NEW draft: same tasks, grader and
// threshold, but a distinct key/title and a reset version so it does not collide
// with the original (workspace+key+version is unique). The target carries over —
// the user retargets it in the form, which also covers "reuse tasks on another
// target".
export function duplicateForm(item: CerebroEval): DraftForm {
  const base = formFromEval(item);
  return { ...base, key: `${base.key}-copy`, title: `${base.title} (copy)`, version: "1.0.0" };
}
