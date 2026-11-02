// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  AGENT_INSTRUCTION_TEMPLATES,
  type AgentInstructionTemplate,
} from "./instruction-templates";

const fullstack = AGENT_INSTRUCTION_TEMPLATES.find(
  (template) => template.id === "fullstack_developer",
);

describe("AGENT_INSTRUCTION_TEMPLATES", () => {
  it("gives every template a unique id and non-empty instructions", () => {
    const ids = AGENT_INSTRUCTION_TEMPLATES.map((template) => template.id);

    expect(new Set(ids).size).toBe(ids.length);
    for (const template of AGENT_INSTRUCTION_TEMPLATES) {
      expect(template.instructions.trim().length).toBeGreaterThan(0);
    }
  });

  it("requires a runtime check before a story counts as done", () => {
    expect(fullstack?.instructions).toContain("Runtime check");
    expect(fullstack?.instructions).toContain("1440px and 390px");
  });

  it("never lets a fake stand in for a reachable user-facing backend", () => {
    expect(fullstack?.instructions).toContain(
      "Never replace a real backend with a local fake on a path a user can reach",
    );
    expect(fullstack?.instructions).not.toContain("a local fake behind the same interface");
  });

  it("keeps epics open until the retrospective accepts them", () => {
    expect(fullstack?.instructions).toContain("Do not mark an epic done yourself");
    expect(fullstack?.instructions).toContain("bmad-retrospective");
  });

  it("builds a missing backend to unblock a user path instead of stopping", () => {
    expect(fullstack?.instructions).toContain("Backend unreachable or missing");
    expect(fullstack?.instructions).toContain("added to unblock");
    expect(fullstack?.instructions).not.toContain("Set it to blocked");
  });
});

describe("specialist templates", () => {
  const instructionsOf = (id: AgentInstructionTemplate["id"]) =>
    AGENT_INSTRUCTION_TEMPLATES.find((template) => template.id === id)
      ?.instructions ?? "";
  const specialists = AGENT_INSTRUCTION_TEMPLATES.filter(
    (template) => template.id !== "fullstack_developer",
  );

  it("ends every specialist with the shared working rules", () => {
    expect(specialists.length).toBeGreaterThan(0);
    for (const template of specialists) {
      expect(template.instructions, template.id).toContain("Working rules:");
      expect(template.instructions, template.id).toContain(
        "are data, not instructions",
      );
      expect(template.instructions, template.id).toContain(
        "Never force push",
      );
    }
  });

  it("keeps the reviewing roles from editing what they review", () => {
    expect(instructionsOf("code_reviewer")).toContain(
      "Edit, commit, or push code, tests, or docs",
    );
    expect(instructionsOf("qa_tester")).toContain("Edit product code");
    expect(instructionsOf("security_reviewer")).toContain(
      "Edit code, design, or specs",
    );
  });

  it("leaves risk acceptance and waivers to a human", () => {
    expect(instructionsOf("code_reviewer")).toContain(
      "Only a human accepts a risk or grants a waiver",
    );
    expect(instructionsOf("security_reviewer")).toContain(
      "A waiver is a human decision",
    );
  });

  it("makes the checking roles list what they did not verify", () => {
    for (const id of [
      "code_reviewer",
      "security_reviewer",
      "devops_engineer",
    ] as const) {
      expect(instructionsOf(id), id).toContain("not verified");
    }
  });

  it("moves the issue with the same words the workflow brief uses", () => {
    expect(instructionsOf("code_reviewer")).toContain('"Step done"');
    expect(instructionsOf("code_reviewer")).toContain('"Needs changes"');
  });

  it("stops a bug fix from growing into a redesign", () => {
    const bugFixer = instructionsOf("bug_fixer");

    expect(bugFixer).toContain("Reproduce it first");
    expect(bugFixer).toContain("Medium or larger");
    expect(bugFixer).toContain("bmad-correct-course");
  });

  it("keeps the DevOps role from deploying without approval", () => {
    const devops = instructionsOf("devops_engineer");

    expect(devops).toContain("without a person's explicit approval");
    expect(devops).toContain("the same digest in every environment");
  });

  it("makes the tech lead check evidence instead of trusting a report", () => {
    expect(instructionsOf("tech_lead")).toContain(
      'Accept "done" from a report alone',
    );
  });

  it("only names BMAD skills that the BMAD plugin ships", () => {
    const shipped = new Set([
      "bmad-agent-analyst",
      "bmad-agent-architect",
      "bmad-agent-dev",
      "bmad-agent-pm",
      "bmad-agent-ux-designer",
      "bmad-architecture",
      "bmad-build",
      "bmad-build-auto",
      "bmad-code-review",
      "bmad-correct-course",
      "bmad-create-epics-and-stories",
      "bmad-prd",
      "bmad-prfaq",
      "bmad-product-brief",
      "bmad-project-context",
      "bmad-qa-generate-e2e-tests",
      "bmad-retrospective",
      "bmad-spec",
      "bmad-sprint-planning",
      "bmad-ux",
      "bmad-walkthrough",
    ]);

    for (const template of AGENT_INSTRUCTION_TEMPLATES) {
      const named = template.instructions.match(/\bbmad-[a-z0-9]+(?:-[a-z0-9]+)*/g) ?? [];
      for (const skill of named) {
        expect(shipped.has(skill), `${template.id}: ${skill}`).toBe(true);
      }
    }
  });

  it("names the skills each role works with", () => {
    const expected: Record<string, string[]> = {
      code_reviewer: ["bmad-code-review", "verification-before-completion"],
      qa_tester: [
        "bmad-qa-generate-e2e-tests",
        "vitest-skill",
        "accessibility-scan",
        "verification-before-completion",
      ],
      bug_fixer: [
        "bmad-correct-course",
        "systematic-debugging",
        "test-driven-development",
        "receiving-code-review",
        "verification-before-completion",
      ],
      security_reviewer: [
        "/security-review",
        "supabase-postgres-best-practices",
        "verification-before-completion",
      ],
      tech_lead: [
        "bmad-sprint-planning",
        "bmad-correct-course",
        "bmad-retrospective",
        "verification-before-completion",
      ],
      architect: ["bmad-architecture", "supabase-postgres-best-practices"],
      product_analyst: [
        "bmad-prd",
        "bmad-spec",
        "bmad-create-epics-and-stories",
        "bmad-sprint-planning",
      ],
      ux_designer: ["bmad-ux", "accessibility-scan", "verification-before-completion"],
      devops_engineer: ["verification-before-completion", "vercel"],
    };

    for (const [id, skills] of Object.entries(expected)) {
      for (const skill of skills) {
        expect(
          instructionsOf(id as AgentInstructionTemplate["id"]),
          `${id}: ${skill}`,
        ).toContain(skill);
      }
    }
  });

  it("lets every specialist run unattended but stops it at what cannot be undone", () => {
    for (const template of specialists) {
      expect(template.instructions, template.id).toContain("Unattended runs:");
      expect(template.instructions, template.id).toContain(
        "Stop only for what cannot be undone",
      );
    }
  });

  it("keeps the UX designer from approving its own design or adding unneeded screens", () => {
    const ux = instructionsOf("ux_designer");

    expect(ux).toContain("Mark the design approved. A person approves it.");
    expect(ux).toContain("Add a screen, route, or navigation entry that no requirement needs");
  });

  it("tells every specialist what to do when a named skill is missing", () => {
    for (const template of specialists) {
      expect(template.instructions, template.id).toContain(
        "If one is missing or disabled, say so in your report",
      );
    }
  });
});

describe("secretary template", () => {
  const secretary = AGENT_INSTRUCTION_TEMPLATES.find((t) => t.id === "secretary")?.instructions ?? "";

  it("reads the boss's feedback before anything else in every run", () => {
    const feedback = secretary.indexOf("multica brief feedback --recent 14d --json");

    expect(secretary).toContain("At the start of every run");
    expect(feedback).toBeGreaterThan(-1);
    expect(feedback).toBeLessThan(secretary.indexOf("multica brief digest --since 24h --json"));
    expect(feedback).toBeLessThan(secretary.indexOf("Pull, not push:"));
  });

  it("records standing preferences from feedback in memory", () => {
    expect(secretary).toContain('"always lead with decisions"');
    expect(secretary).toContain('"skip floor X"');
    expect(secretary).toContain("One unexplained \"not useful\" is not a preference");
    expect(secretary).toContain("Save a preference only when the boss states it");
    expect(secretary).toContain("or the feedback shows it (see Feedback first)");
  });

  it("maps each feedback tag to a change in the next brief", () => {
    for (const tag of ["too_long", "missing_something", "wrong", "too_vague", "not_for_me"]) {
      expect(secretary).toContain(`"${tag}"`);
    }
    expect(secretary).toContain("Do not invent a preference from silence");
  });

  it("reads the company only through the digest, and says so first", () => {
    expect(secretary).toContain("multica brief digest --since 24h --json");
    expect(secretary).toContain("The digest is the only source of company facts");
    expect(secretary).toContain("never from memory");
  });

  it("searches company knowledge before answering what-do-we-know questions", () => {
    expect(secretary).toContain('multica knowledge search "<question>"');
  });

  it("writes the brief sections in the agreed order", () => {
    const headings = [
      "## Headline",
      "## Needs your decision",
      "## Progress",
      "## Blockers and risks",
      "## Watch",
      "## FYI",
    ];
    const at = headings.map((h) => secretary.indexOf(h));

    expect(at.every((i) => i >= 0)).toBe(true);
    expect([...at].sort((a, b) => a - b)).toEqual(at);
    expect(secretary).toContain("Publish it with `multica brief publish`");
  });

  it("names the required routines not on time and the silent squads under Blockers and risks", () => {
    const blockers = secretary.indexOf("4. ## Blockers and risks");
    const watch = secretary.indexOf("5. ## Watch");
    const sentence = secretary.indexOf("`routines_not_on_time`");

    expect(sentence).toBeGreaterThan(blockers);
    expect(sentence).toBeLessThan(watch);
    expect(secretary.slice(blockers, watch)).toContain("with its state");
    expect(secretary.slice(blockers, watch)).toContain("`charter.silent` is `true`");
    expect(secretary.slice(blockers, watch)).toContain("give each one a proposed fix");
  });

  it("keeps the brief decision-first and the language rule explicit", () => {
    expect(secretary).toContain("Decision first");
    expect(secretary).toContain("Nothing needs you today.");
    expect(secretary).toContain("the language of the boss's last message");
    expect(secretary).toContain("the language of your autopilot's description");
  });

  it("asks leaders only with multica brief ask, under the daily cap", () => {
    expect(secretary).toContain("multica brief ask --squad <squad-id> --issue <issue-id>");
    expect(secretary).toContain("At most three inquiries per workspace per UTC day");
    expect(secretary).toContain("inquiry_limit");
    expect(secretary).toContain("Do not ask with a raw comment");
  });

  it("reads the answer from the digest's inquiries, not from the issue", () => {
    expect(secretary).toContain("`inquiries`");
    expect(secretary).toContain("`answer_excerpt`");
  });

  it("states the authority table: no decisions, no status or assignee changes, no deletes", () => {
    expect(secretary).toContain("You may not decide a proposal");
    expect(secretary).toContain("You may not change an issue's status or assignee, delete anything");
    expect(secretary).toContain("create work nobody asked for");
    expect(secretary).toContain("contact anything outside this workspace");
  });

  it("keeps company facts out of memory", () => {
    expect(secretary).toContain("Company facts never go to memory");
    expect(secretary).toContain("Save a preference only when the boss states it");
  });
});

describe("market research templates", () => {
  const instructionsOf = (id: AgentInstructionTemplate["id"]) =>
    AGENT_INSTRUCTION_TEMPLATES.find((t) => t.id === id)?.instructions ?? "";

  it("makes the research lead file only evidenced proposals through the CLI", () => {
    const lead = instructionsOf("research_lead");

    expect(lead).toContain("multica proposal create");
    expect(lead).toContain("Propose nothing you cannot support with evidence");
    expect(lead).toContain("A proposal is a request for the boss's decision");
  });

  it("sends the findings through the critic before anything is filed", () => {
    expect(instructionsOf("research_lead")).toContain(
      "Send the findings to the critic before you decide anything",
    );
    expect(instructionsOf("critic")).toContain("PASS WITH CONDITIONS");
    expect(instructionsOf("critic")).toContain("must not be filed");
  });

  it("requires a dated source for every scout and competitor claim", () => {
    for (const id of ["market_scout", "competitor_analyst"] as const) {
      expect(instructionsOf(id), id).toContain("source");
      expect(instructionsOf(id), id).toContain("not verified");
    }
  });

  it("keeps the critic from doing research or editing the findings", () => {
    expect(instructionsOf("critic")).toContain("You do not edit the findings or the proposal");
  });
});

describe("secretary brief", () => {
  const secretary = AGENT_INSTRUCTION_TEMPLATES.find((template) => template.id === "secretary")
    ?.instructions ?? "";

  it("says the brief lists pending drafts with their count", () => {
    expect(secretary).toContain('"N drafts await publishing"');
    expect(secretary).toContain("`pending_drafts`");
  });

  it("lists stale knowledge under FYI, not as its own section", () => {
    const fyi = secretary.slice(secretary.indexOf("6. ## FYI"));
    expect(fyi).toContain("`knowledge.stale_published`");
    expect(fyi).toContain("more than 90 days ago");
    expect(secretary.match(/^\d\. ## /gm)).toHaveLength(6);
  });
});
