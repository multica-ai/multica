import { describe, expect, it } from "vitest";
import {
  applyPastedTextStagingResult,
  agentComposerDraftDisplayPrompt,
  agentComposerDraftFiles,
  agentComposerDraftHasContent,
  agentComposerDraftCanSubmit,
  agentComposerDraftImages,
  agentComposerDraftLargeTexts,
  agentComposerDraftPrompt,
  agentComposerDraftQuotes,
  agentComposerDraftSubmittedText,
  agentComposerDraftToPromptContent,
  agentPromptContentDisplayText,
  agentPromptContentToComposerDraft,
  buildAgentComposerDraft,
  appendAgentComposerDraftQuote,
  agentComposerDraftPreservingConnectors,
  emptyAgentComposerDraft,
  extractPastedTextArchivePaths,
  linkifyPastedTextReferences,
  normalizeAgentPromptContentBlocks,
  projectAgentComposerDraftSubmission,
  updateAgentComposerDraft
} from "./agentComposerDraft";
import { createAgentComposerFileMentionMarkdown } from "../agentRichText/agentMentionMarkdown";

describe("agentComposerDraft", () => {
  it("keeps transcript selections as visible draft quotes and submits blockquotes", () => {
    const draft = appendAgentComposerDraftQuote(
      buildAgentComposerDraft({ prompt: "Why does this happen?" }),
      {
        type: "quote",
        id: "quote-1",
        text: "First line\nSecond line"
      }
    );

    expect(agentComposerDraftQuotes(draft)).toEqual([
      { type: "quote", id: "quote-1", text: "First line\nSecond line" }
    ]);
    expect(agentComposerDraftHasContent(draft)).toBe(true);
    expect(projectAgentComposerDraftSubmission({ draft, skills: [] })).toEqual({
      content: [
        { type: "text", text: "> First line\n> Second line\n\nWhy does this happen?" }
      ],
      displayPrompt: "> First line\n> Second line\n\nWhy does this happen?"
    });
    expect(
      agentComposerDraftQuotes(
        updateAgentComposerDraft(draft, { prompt: "A revised question" })
      )
    ).toEqual([
      { type: "quote", id: "quote-1", text: "First line\nSecond line" }
    ]);
  });

  it("retains a quote-only draft as content but requires a question before sending", () => {
    const quoted = buildAgentComposerDraft({ prompt: " ", quotes: [{ type: "quote", id: "q", text: "Context" }] });
    expect(agentComposerDraftHasContent(quoted)).toBe(true);
    expect(agentComposerDraftCanSubmit(quoted)).toBe(false);
    expect(agentComposerDraftCanSubmit(updateAgentComposerDraft(quoted, { prompt: "Why?" }))).toBe(true);
    expect(agentComposerDraftCanSubmit(buildAgentComposerDraft({ prompt: "", images: [{ id: "img", mimeType: "image/png", name: "image.png", url: "https://example.com/image.png" }] }))).toBe(true);
  });

  it("deduplicates the same selected transcript quote", () => {
    const initial = buildAgentComposerDraft({ prompt: "" });
    const once = appendAgentComposerDraftQuote(initial, {
      type: "quote",
      id: "quote-1",
      text: " selected context "
    });
    const twice = appendAgentComposerDraftQuote(once, {
      type: "quote",
      id: "quote-2",
      text: "selected context"
    });

    expect(agentComposerDraftQuotes(twice)).toEqual([
      { type: "quote", id: "quote-1", text: "selected context" }
    ]);
  });

  it("stores text, images, files, and pasted text in one ordered content array", () => {
    const draft = buildAgentComposerDraft({
      prompt: "Inspect this",
      images: [
        {
          id: "image-1",
          name: "screen.png",
          mimeType: "image/png",
          previewUrl: "blob:image-1"
        }
      ],
      files: [{ id: "file-1", name: "notes.md" }],
      largeTexts: [
        { id: "paste-1", name: "pasted-text.txt", text: "long body" }
      ]
    });

    expect(draft.map((block) => block.type)).toEqual([
      "text",
      "image",
      "file",
      "file"
    ]);
    expect(draft[2]).toMatchObject({ type: "file", kind: "file" });
    expect(draft[3]).toMatchObject({
      type: "file",
      kind: "pasted-text",
      text: "long body"
    });
  });

  it("preserves safe URL-only image blocks and rejects unsafe or ambiguous sources", () => {
    const signedUrl = "https://bucket.example/image.png?token=secret";
    expect(
      normalizeAgentPromptContentBlocks([
        {
          type: "image",
          mimeType: "image/png",
          url: ` ${signedUrl} `,
          attachmentId: " attachment-1 ",
          name: " screenshot.png "
        }
      ])
    ).toEqual([
      {
        type: "image",
        mimeType: "image/png",
        url: signedUrl,
        attachmentId: "attachment-1",
        name: "screenshot.png"
      }
    ]);
    expect(
      normalizeAgentPromptContentBlocks([
        {
          type: "image",
          mimeType: "image/png",
          url: "http://example.com/a.png"
        },
        {
          type: "image",
          mimeType: "image/png",
          url: signedUrl,
          data: "aW1hZ2U="
        }
      ])
    ).toEqual([]);
  });

  it("materializes prepared URL file locators as Markdown links", () => {
    const content = [
      {
        type: "file" as const,
        name: "shared.txt",
        mimeType: "text/plain",
        sizeBytes: 12,
        assetId: "asset-1",
        uri: "object://shared.txt",
        url: "https://assets.example/shared.txt",
        uploadStatus: "uploaded",
        kind: "file"
      }
    ];

    const draft = agentPromptContentToComposerDraft(content, "restored");

    expect(agentComposerDraftFiles(draft)).toEqual([
      expect.objectContaining({
        assetId: "asset-1",
        uri: "object://shared.txt",
        url: "https://assets.example/shared.txt",
        uploadStatus: "uploaded"
      })
    ]);
    expect(agentComposerDraftToPromptContent({ draft, skills: [] })).toEqual([
      {
        type: "text",
        text: "[@shared.txt](https://assets.example/shared.txt)"
      }
    ]);
  });

  it("projects composer files at their inline mention positions", () => {
    const firstMention = createAgentComposerFileMentionMarkdown({
      id: "file-1",
      name: "first.pdf",
      status: "ready"
    });
    const secondMention = createAgentComposerFileMentionMarkdown({
      id: "file-2",
      name: "second.txt",
      status: "ready"
    });
    const draft = buildAgentComposerDraft({
      prompt: `before ${firstMention} middle ${secondMention} after`,
      files: [
        { id: "file-1", name: "first.pdf", path: "/runtime/first.pdf" },
        { id: "file-2", name: "second.txt", path: "/runtime/second.txt" }
      ]
    });

    expect(agentComposerDraftToPromptContent({ draft, skills: [] })).toEqual([
      {
        type: "text",
        text: "before [@first.pdf](/runtime/first.pdf) middle [@second.txt](/runtime/second.txt) after"
      }
    ]);
  });

  it("materializes composer-file mentions out of the persisted display prompt", () => {
    const mention = createAgentComposerFileMentionMarkdown({
      id: "file-1",
      name: "report.pdf",
      status: "ready"
    });
    const draft = buildAgentComposerDraft({
      prompt: `summarize ${mention}`,
      files: [
        {
          id: "file-1",
          name: "report.pdf",
          path: "/runtime/report.pdf"
        }
      ]
    });

    expect(projectAgentComposerDraftSubmission({ draft, skills: [] })).toEqual({
      content: [
        {
          type: "text",
          text: "summarize [@report.pdf](/runtime/report.pdf)"
        }
      ]
    });
    expect(agentComposerDraftDisplayPrompt(draft)).toBeUndefined();
  });

  it("keeps non-file mention display prompts while materializing composer files", () => {
    const fileMention = createAgentComposerFileMentionMarkdown({
      id: "file-1",
      name: "notes.md",
      status: "ready"
    });
    const draft = buildAgentComposerDraft({
      prompt: `/review-code ${fileMention}`,
      files: [
        {
          id: "file-1",
          name: "notes.md",
          path: "/runtime/notes.md"
        }
      ]
    });

    expect(
      projectAgentComposerDraftSubmission({
        draft,
        skills: [
          {
            name: "review-code",
            trigger: "$review-code",
            invocation: "promptItem",
            sourceKind: "personal",
            path: "/skills/review-code/SKILL.md"
          }
        ]
      })
    ).toEqual({
      content: [
        {
          type: "text",
          text: "$review-code [@notes.md](/runtime/notes.md)"
        },
        {
          type: "skill",
          name: "review-code",
          path: "/skills/review-code/SKILL.md"
        }
      ],
      displayPrompt: `/review-code [@notes.md](/runtime/notes.md)`
    });
  });

  it("does not submit or count a file registry entry without an editor mention", () => {
    const draft = buildAgentComposerDraft({
      prompt: "",
      files: [{ id: "file-1", name: "orphan.pdf", path: "/runtime/orphan.pdf" }]
    });

    expect(agentComposerDraftHasContent(draft)).toBe(false);
    expect(agentComposerDraftToPromptContent({ draft, skills: [] })).toEqual(
      []
    );
  });

  it("does not count a composer file mention without a registry entry", () => {
    const draft = buildAgentComposerDraft({
      prompt: createAgentComposerFileMentionMarkdown({
        id: "missing-file",
        name: "missing.pdf",
        status: "ready"
      })
    });

    expect(agentComposerDraftHasContent(draft)).toBe(false);
  });

  it("restores ordered file blocks as inline composer mentions and resubmits Markdown", () => {
    const content = [
      { type: "text" as const, text: "before" },
      {
        type: "file" as const,
        kind: "file",
        name: "report.pdf",
        path: "/runtime/report.pdf"
      },
      { type: "text" as const, text: "after" }
    ];
    const draft = agentPromptContentToComposerDraft(content, "queued");

    expect(agentComposerDraftPrompt(draft)).toContain(
      "mention://composer-file/queued:file:0"
    );
    expect(agentComposerDraftToPromptContent({ draft, skills: [] })).toEqual([
      {
        type: "text",
        text: "before[@report.pdf](/runtime/report.pdf)after"
      }
    ]);
  });

  it("keeps only connector blocks when preserving a submitted draft", () => {
    const draft = buildAgentComposerDraft({
      prompt: "list events",
      images: [
        {
          id: "image-1",
          name: "screen.png",
          mimeType: "image/png",
          previewUrl: "blob:image-1"
        }
      ],
      files: [{ id: "file-1", name: "notes.md" }],
      connectors: [{ connectorKey: "lark-cli" }, { connectorKey: "github" }]
    });

    expect(agentComposerDraftPreservingConnectors(draft)).toEqual(
      buildAgentComposerDraft({
        prompt: "",
        connectors: [{ connectorKey: "lark-cli" }, { connectorKey: "github" }]
      })
    );
    expect(agentComposerDraftPreservingConnectors(undefined)).toEqual(
      emptyAgentComposerDraft()
    );
  });

  it("normalizes empty drafts", () => {
    const draft = emptyAgentComposerDraft();

    expect(agentComposerDraftHasContent(draft)).toBe(false);
    expect(
      agentComposerDraftToPromptContent({
        draft,
        skills: []
      })
    ).toEqual([]);
  });

  it("converts text-only drafts into prompt content", () => {
    expect(
      agentComposerDraftToPromptContent({
        draft: buildAgentComposerDraft({ prompt: "  run tests  " }),
        skills: []
      })
    ).toEqual([{ type: "text", text: "run tests" }]);
  });

  it("preserves structured mentions as the conversation display prompt", () => {
    const prompt =
      "[@a](mention://browser-element/browser-element%3A1?path=%2Ftmp%2Fa.txt&tag=a&workspaceId=workspace-1) " +
      "[@div](mention://browser-element/browser-element%3A2?path=%2Ftmp%2Fdiv.txt&tag=div&workspaceId=workspace-1) 22222222";

    expect(
      agentComposerDraftDisplayPrompt(buildAgentComposerDraft({ prompt }))
    ).toBe(prompt);
    expect(
      agentComposerDraftDisplayPrompt(
        buildAgentComposerDraft({ prompt: "ordinary text" })
      )
    ).toBeUndefined();
  });

  it("builds prompt-item blocks from the skill invocation contract", () => {
    expect(
      agentComposerDraftToPromptContent({
        draft: buildAgentComposerDraft({
          prompt: "/review-code inspect this"
        }),
        skills: [
          {
            name: "review-code",
            trigger: "$review-code",
            invocation: "promptItem",
            sourceKind: "personal",
            path: "/skills/review-code/SKILL.md"
          }
        ]
      })
    ).toEqual([
      { type: "text", text: "$review-code inspect this" },
      {
        type: "skill",
        name: "review-code",
        path: "/skills/review-code/SKILL.md"
      }
    ]);
  });

  it("projects prompt-item aliases into runtime content while preserving visible text", () => {
    expect(
      projectAgentComposerDraftSubmission({
        draft: buildAgentComposerDraft({
          prompt: "/review-code inspect this"
        }),
        skills: [
          {
            name: "review-code",
            trigger: "$review-code",
            invocation: "promptItem",
            sourceKind: "personal",
            path: "/skills/review-code/SKILL.md"
          }
        ]
      })
    ).toEqual({
      content: [
        { type: "text", text: "$review-code inspect this" },
        {
          type: "skill",
          name: "review-code",
          path: "/skills/review-code/SKILL.md"
        }
      ],
      displayPrompt: "/review-code inspect this"
    });
  });

  it("projects text-trigger aliases into runtime content while preserving visible text", () => {
    expect(
      projectAgentComposerDraftSubmission({
        draft: buildAgentComposerDraft({ prompt: "$foo inspect this" }),
        skills: [
          {
            name: "foo",
            trigger: "/foo",
            invocation: "textTrigger",
            sourceKind: "plugin"
          }
        ]
      })
    ).toEqual({
      content: [{ type: "text", text: "/foo inspect this" }],
      displayPrompt: "$foo inspect this"
    });
  });

  it("projects a selected local connector into a structured prompt block", () => {
    expect(
      projectAgentComposerDraftSubmission({
        draft: buildAgentComposerDraft({
          prompt: "/lark-cli list my calendar events"
        }),
        skills: [
          {
            name: "lark-cli",
            trigger: "/lark-cli",
            connectorKey: "lark-cli",
            invocation: "textTrigger",
            sourceKind: "connector",
            kind: "connector",
            status: "available"
          }
        ]
      })
    ).toEqual({
      content: [
        { type: "text", text: "list my calendar events" },
        { type: "connector", connectorKey: "lark-cli" }
      ],
      displayPrompt: "/lark-cli list my calendar events"
    });
  });

  it("allows a local connector-only submission", () => {
    expect(
      projectAgentComposerDraftSubmission({
        draft: buildAgentComposerDraft({ prompt: "/lark-cli" }),
        skills: [
          {
            name: "lark-cli",
            trigger: "/lark-cli",
            connectorKey: "lark-cli",
            invocation: "textTrigger",
            sourceKind: "connector",
            kind: "connector",
            status: "available"
          }
        ]
      })
    ).toEqual({
      content: [{ type: "connector", connectorKey: "lark-cli" }],
      displayPrompt: "/lark-cli"
    });
  });

  it("omits display prompts for native skill prefixes and ordinary text", () => {
    const skill = {
      name: "review-code",
      trigger: "$review-code",
      invocation: "promptItem" as const,
      sourceKind: "personal" as const,
      path: "/skills/review-code/SKILL.md"
    };

    expect(
      projectAgentComposerDraftSubmission({
        draft: buildAgentComposerDraft({
          prompt: "$review-code inspect this"
        }),
        skills: [skill]
      })
    ).toEqual({
      content: [
        { type: "text", text: "$review-code inspect this" },
        {
          type: "skill",
          name: "review-code",
          path: "/skills/review-code/SKILL.md"
        }
      ]
    });
    expect(
      projectAgentComposerDraftSubmission({
        draft: buildAgentComposerDraft({ prompt: "inspect this" }),
        skills: [skill]
      })
    ).toEqual({ content: [{ type: "text", text: "inspect this" }] });
  });

  it("prefers an explicit rich display prompt over derived submission text", () => {
    const draft = buildAgentComposerDraft({
      prompt: "Summarize this",
      largeTexts: [
        {
          id: "11111111-1111-4111-8111-111111111111",
          name: "pasted-text.txt",
          text: "first line\nsecond line",
          sizeBytes: 22,
          path: "/archive/aa/deadbeef.txt"
        }
      ]
    });

    expect(projectAgentComposerDraftSubmission({ draft, skills: [] })).toEqual({
      content: [
        { type: "text", text: "Summarize this" },
        {
          type: "file",
          kind: "pasted-text",
          path: "/archive/aa/deadbeef.txt",
          name: "first line…",
          sizeBytes: 22
        }
      ],
      displayPrompt:
        "Summarize this\n[@first line…](mention://pasted-text/11111111-1111-4111-8111-111111111111?path=%2Farchive%2Faa%2Fdeadbeef.txt&size=22)"
    });
  });

  it("projects remote pasted text as a safe display-only mention", () => {
    const signedUrl =
      "https://assets.example/shared/pasted.txt?signature=short-lived";
    const staged = applyPastedTextStagingResult(
      {
        id: "22222222-2222-4222-8222-222222222222",
        name: "pasted-text.txt",
        text: "first line shared",
        sizeBytes: 22,
        uploading: true
      },
      {
        name: "pasted-text.txt",
        sizeBytes: 22,
        assetId: "asset-1",
        uri: "object://shared/pasted.txt",
        uploadStatus: "uploaded",
        url: signedUrl
      }
    );
    const draft = buildAgentComposerDraft({
      prompt: "",
      largeTexts: [staged]
    });

    const submission = projectAgentComposerDraftSubmission({
      draft,
      skills: []
    });

    expect(submission).toEqual({
      content: [
        {
          type: "file",
          kind: "pasted-text",
          mimeType: undefined,
          name: "first line…",
          sizeBytes: 22,
          assetId: "asset-1",
          uri: "object://shared/pasted.txt",
          uploadStatus: "uploaded",
          url: signedUrl
        }
      ],
      displayPrompt:
        "[@first line…](mention://pasted-text/22222222-2222-4222-8222-222222222222?presentation=remote)"
    });
    expect(submission.displayPrompt).not.toContain(signedUrl);
    expect(submission.displayPrompt).not.toContain("asset-1");
    expect(submission.displayPrompt).not.toContain("object://");
  });

  it("submits a landed pasted-text draft as a structured file block", () => {
    const draft = buildAgentComposerDraft({
      prompt: "Summarize this",
      images: [],
      largeTexts: [
        {
          id: "11111111-1111-4111-8111-111111111111",
          name: "pasted-text.txt",
          text: "first line\nsecond line",
          sizeBytes: 22,
          path: "/archive/aa/deadbeef.txt"
        }
      ]
    });

    expect(agentComposerDraftHasContent(draft)).toBe(true);
    // The conversation-flow display prompt encodes the landed pasted text as a
    // pasted-text mention link (path + size in the href) so the host can render
    // a clickable chip; the raw text body never enters it.
    expect(agentComposerDraftDisplayPrompt(draft)).toBe(
      "Summarize this\n[@first line…](mention://pasted-text/11111111-1111-4111-8111-111111111111?path=%2Farchive%2Faa%2Fdeadbeef.txt&size=22)"
    );
    expect(
      agentComposerDraftToPromptContent({
        draft,
        skills: []
      })
    ).toEqual([
      { type: "text", text: "Summarize this" },
      {
        type: "file",
        kind: "pasted-text",
        path: "/archive/aa/deadbeef.txt",
        // Block name carries the preview (first chars + ellipsis) so the
        // send-time instruction can persist it in content.
        name: "first line…",
        sizeBytes: 22
      }
    ]);
    // No translated instruction and no inlined body enter the submitted text.
    expect(agentComposerDraftSubmittedText(draft)).toBe("Summarize this");
  });

  it("keeps an uploading or errored pasted-text draft out of submit content", () => {
    const uploading = buildAgentComposerDraft({
      prompt: "",
      images: [],
      largeTexts: [
        {
          id: "22222222-2222-4222-8222-222222222222",
          name: "pasted-text.txt",
          text: "still uploading",
          uploading: true
        }
      ]
    });
    const errored = buildAgentComposerDraft({
      prompt: "",
      images: [],
      largeTexts: [
        {
          id: "33333333-3333-4333-8333-333333333333",
          name: "pasted-text.txt",
          text: "failed",
          uploadError: "disk full"
        }
      ]
    });

    // A pending/errored chip still counts as content (so the composer is not
    // treated as empty) but never lands in the submitted prompt content.
    expect(agentComposerDraftHasContent(uploading)).toBe(true);
    expect(
      agentComposerDraftToPromptContent({
        draft: uploading,
        skills: []
      })
    ).toEqual([]);
    expect(
      agentComposerDraftToPromptContent({
        draft: errored,
        skills: []
      })
    ).toEqual([]);
  });

  it("restores a pasted-text file block back into a large-text chip", () => {
    const draft = agentPromptContentToComposerDraft(
      [
        { type: "text", text: "Summarize this" },
        {
          type: "file",
          kind: "pasted-text",
          path: "/archive/aa/deadbeef.txt",
          name: "pasted-text.txt",
          sizeBytes: 22
        }
      ],
      "restore-1"
    );

    expect(agentComposerDraftPrompt(draft)).toBe("Summarize this");
    expect(agentComposerDraftFiles(draft)).toEqual([]);
    expect(agentComposerDraftLargeTexts(draft)).toHaveLength(1);
    expect(agentComposerDraftLargeTexts(draft)[0]).toMatchObject({
      text: "",
      path: "/archive/aa/deadbeef.txt"
    });
    expect(agentComposerDraftHasContent(draft)).toBe(true);
    expect(agentComposerDraftLargeTexts(draft)[0]).toMatchObject({
      id: "restore-1:pasted-text:0",
      name: "pasted-text.txt",
      path: "/archive/aa/deadbeef.txt",
      sizeBytes: 22,
      text: ""
    });
  });

  it("materializes the codex-style instruction only at send time", async () => {
    const { materializePastedTextInstructions } =
      await import("./agentComposerDraft");
    const content = [
      { type: "text" as const, text: "Summarize this" },
      {
        type: "file" as const,
        kind: "pasted-text",
        path: "/archive/aa/deadbeef.txt",
        name: "first line"
      }
    ];

    // The pasted-text file block is stripped and replaced by the instruction
    // text (preview quoted + path embedded); no file block survives.
    expect(
      materializePastedTextInstructions(content, {
        header: () => "Referenced pasted text files:",
        line: (preview, path) =>
          `- pasted text file "${preview}": ${path}. Read this file before continuing.`
      })
    ).toEqual([
      { type: "text", text: "Summarize this" },
      {
        type: "text",
        text: 'Referenced pasted text files:\n- pasted text file "first line": /archive/aa/deadbeef.txt. Read this file before continuing.'
      }
    ]);

    // No pasted-text blocks → content returned unchanged.
    expect(
      materializePastedTextInstructions([{ type: "text", text: "hi" }], {
        header: () => "H",
        line: (_preview, path) => path
      })
    ).toEqual([{ type: "text", text: "hi" }]);
  });

  it("keeps remote pasted text on the prepared-file attachment path", async () => {
    const { materializePastedTextInstructions } =
      await import("./agentComposerDraft");
    const content = [
      { type: "text" as const, text: "Summarize this" },
      {
        type: "file" as const,
        kind: "pasted-text",
        mimeType: "text/plain;charset=utf-8",
        name: "first line",
        sizeBytes: 22,
        assetId: "asset-1",
        uri: "object://shared/pasted.txt",
        uploadStatus: "uploaded",
        url: "https://assets.example/shared/pasted.txt?signature=short-lived"
      }
    ];

    expect(
      materializePastedTextInstructions(content, {
        header: () => "Referenced pasted text files:",
        line: (preview, path) => `${preview}: ${path}`
      })
    ).toEqual([
      { type: "text", text: "Summarize this" },
      {
        type: "file",
        kind: "file",
        mimeType: "text/plain;charset=utf-8",
        name: "first line",
        sizeBytes: 22,
        assetId: "asset-1",
        uri: "object://shared/pasted.txt",
        uploadStatus: "uploaded",
        url: "https://assets.example/shared/pasted.txt?signature=short-lived"
      }
    ]);

    const restored = agentPromptContentToComposerDraft(
      content,
      "remote-restore"
    );
    expect(agentComposerDraftHasContent(restored)).toBe(true);
    expect(agentComposerDraftLargeTexts(restored)).toEqual([
      expect.objectContaining({
        url: "https://assets.example/shared/pasted.txt?signature=short-lived",
        assetId: "asset-1",
        uri: "object://shared/pasted.txt",
        uploadStatus: "uploaded"
      })
    ]);
  });

  it("adds codex app-server prompt items for referenced skills and connectors", () => {
    expect(
      agentComposerDraftToPromptContent({
        draft: buildAgentComposerDraft({
          prompt: "$review $github check this"
        }),
        skills: [
          {
            name: "review",
            trigger: "$review",
            invocation: "promptItem",
            sourceKind: "plugin",
            path: "/tmp/review/SKILL.md",
            kind: "skill"
          },
          {
            name: "GitHub",
            trigger: "$github",
            invocation: "promptItem",
            sourceKind: "connector",
            path: "app://github",
            kind: "connector"
          }
        ]
      })
    ).toEqual([
      { type: "text", text: "$review $github check this" },
      { type: "skill", name: "review", path: "/tmp/review/SKILL.md" },
      { type: "mention", name: "GitHub", path: "app://github" }
    ]);
  });

  it("converts image-only drafts into prompt content", () => {
    expect(
      agentComposerDraftToPromptContent({
        draft: buildAgentComposerDraft({
          prompt: "",
          images: [
            {
              id: "image-1",
              name: "screen.png",
              mimeType: "image/png",
              data: "aW1hZ2U=",
              previewUrl: "data:image/png;base64,aW1hZ2U="
            }
          ]
        }),
        skills: []
      })
    ).toEqual([
      {
        type: "image",
        mimeType: "image/png",
        data: "aW1hZ2U=",
        name: "screen.png"
      }
    ]);
  });

  it("round-trips HTTPS URL image drafts without hydration", () => {
    const url = "https://bucket.example/screen.webp?X-Amz-Signature=secret";
    const draft = agentPromptContentToComposerDraft(
      [
        {
          type: "image",
          mimeType: "image/webp",
          url,
          attachmentId: "attachment-remote",
          name: "screen.webp"
        }
      ],
      "remote"
    );

    expect(agentComposerDraftImages(draft)).toEqual([
      {
        id: "remote:image:0",
        name: "screen.webp",
        mimeType: "image/webp",
        attachmentId: "attachment-remote",
        url,
        previewUrl: url
      }
    ]);
    expect(
      agentComposerDraftToPromptContent({
        draft,
        skills: []
      })
    ).toEqual([
      {
        type: "image",
        mimeType: "image/webp",
        attachmentId: "attachment-remote",
        url,
        name: "screen.webp"
      }
    ]);
  });

  it("round-trips URL-only image drafts when editing queued content", () => {
    const url = "https://bucket.example/queued.png?X-Amz-Signature=secret";
    const draft = agentPromptContentToComposerDraft(
      [
        {
          type: "image",
          mimeType: "image/png",
          url,
          name: "queued.png"
        }
      ],
      "queued"
    );

    expect(agentComposerDraftImages(draft)).toEqual([
      {
        id: "queued:image:0",
        name: "queued.png",
        mimeType: "image/png",
        url,
        previewUrl: url
      }
    ]);
    expect(
      agentComposerDraftToPromptContent({
        draft,
        skills: []
      })
    ).toEqual([
      {
        type: "image",
        mimeType: "image/png",
        url,
        name: "queued.png"
      }
    ]);
  });

  it("converts attachment-backed image-only drafts into prompt content", () => {
    expect(
      agentComposerDraftToPromptContent({
        draft: buildAgentComposerDraft({
          prompt: "",
          images: [
            {
              id: "image-1",
              name: "screen.png",
              mimeType: "image/png",
              attachmentId: "attachment-1",
              previewUrl: "data:image/png;base64,aW1hZ2U="
            }
          ]
        }),
        skills: []
      })
    ).toEqual([
      {
        type: "image",
        mimeType: "image/png",
        attachmentId: "attachment-1",
        name: "screen.png"
      }
    ]);
  });

  it("converts staged image drafts into path prompt content", () => {
    expect(
      agentComposerDraftToPromptContent({
        draft: buildAgentComposerDraft({
          prompt: "",
          images: [
            {
              id: "image-1",
              name: "screen.png",
              mimeType: "image/png",
              path: "/var/cache/tsh/local-assets/workspace-1/user-1/screen.png",
              previewUrl: "data:image/png;base64,aW1hZ2U="
            }
          ]
        }),
        skills: []
      })
    ).toEqual([
      {
        type: "image",
        mimeType: "image/png",
        path: "/var/cache/tsh/local-assets/workspace-1/user-1/screen.png",
        name: "screen.png"
      }
    ]);
  });

  it("does not emit image drafts that are still uploading or failed", () => {
    expect(
      agentComposerDraftToPromptContent({
        draft: buildAgentComposerDraft({
          prompt: "",
          images: [
            {
              id: "image-1",
              name: "uploading.png",
              mimeType: "image/png",
              data: "aW1hZ2U=",
              previewUrl: "data:image/png;base64,aW1hZ2U=",
              uploading: true
            },
            {
              id: "image-2",
              name: "failed.png",
              mimeType: "image/png",
              data: "aW1hZ2U=",
              previewUrl: "data:image/png;base64,aW1hZ2U=",
              uploadError: "failed"
            }
          ]
        }),
        skills: []
      })
    ).toEqual([]);
  });

  it("restores text and image content into stable draft ids", () => {
    const draft = agentPromptContentToComposerDraft(
      [
        { type: "text", text: "describe this" },
        {
          type: "image",
          mimeType: "image/png",
          data: "aW1hZ2U=",
          name: "panel.png"
        }
      ],
      "restore-queued-1"
    );

    expect(draft).toEqual(
      buildAgentComposerDraft({
        prompt: "describe this",
        files: [],
        images: [
          {
            id: "restore-queued-1:image:0",
            name: "panel.png",
            mimeType: "image/png",
            data: "aW1hZ2U=",
            previewUrl: "data:image/png;base64,aW1hZ2U="
          }
        ]
      })
    );
  });

  it("restores path image content into stable draft ids", () => {
    const draft = agentPromptContentToComposerDraft(
      [
        {
          type: "image",
          mimeType: "image/png",
          path: "/var/cache/tsh/local-assets/workspace-1/user-1/panel.png",
          name: "panel.png"
        }
      ],
      "restore-queued-1"
    );

    expect(draft).toEqual(
      buildAgentComposerDraft({
        prompt: "",
        files: [],
        images: [
          {
            id: "restore-queued-1:image:0",
            name: "panel.png",
            mimeType: "image/png",
            path: "/var/cache/tsh/local-assets/workspace-1/user-1/panel.png",
            previewUrl: ""
          }
        ]
      })
    );
  });

  it("restores attachment-backed image content into stable draft ids", () => {
    const draft = agentPromptContentToComposerDraft(
      [
        {
          type: "image",
          mimeType: "image/png",
          attachmentId: "attachment-1",
          name: "panel.png"
        }
      ],
      "restore-queued-1"
    );

    expect(draft).toEqual(
      buildAgentComposerDraft({
        prompt: "",
        files: [],
        images: [
          {
            id: "restore-queued-1:image:0",
            name: "panel.png",
            mimeType: "image/png",
            attachmentId: "attachment-1",
            previewUrl: ""
          }
        ]
      })
    );
  });

  it("converts local file drafts into prompt content", () => {
    expect(
      agentComposerDraftToPromptContent({
        draft: buildAgentComposerDraft({
          prompt: createAgentComposerFileMentionMarkdown({
            id: "file-1",
            name: "report.pdf",
            status: "ready"
          }),
          images: [],
          files: [
            {
              id: "file-1",
              name: "report.pdf",
              mimeType: "application/pdf",
              path: "/var/cache/tsh/local-assets/workspace-1/user-1/report.pdf",
              hostPath: "/Users/me/report.pdf",
              assetId: "asset-1",
              sizeBytes: 42
            }
          ]
        }),
        skills: []
      })
    ).toEqual([
      {
        type: "text",
        text: "[@report.pdf](/var/cache/tsh/local-assets/workspace-1/user-1/report.pdf)"
      }
    ]);
  });

  it("derives display text from text content only", () => {
    expect(
      agentPromptContentDisplayText([
        { type: "text", text: "first" },
        { type: "image", mimeType: "image/png", data: "aW1hZ2U=" },
        { type: "text", text: "second" }
      ])
    ).toBe("first\nsecond");
  });

  it("extracts landed pasted-text archive paths from persisted instruction text (paths may contain spaces)", () => {
    const text =
      "Referenced pasted text files:\n" +
      '- pasted text file "first line": /Users/z/Library/Application Support/Tutti-dev/agent-prompt-assets/room-1/ab/deadbeef.txt. Read this file before continuing.';
    expect(extractPastedTextArchivePaths(text)).toEqual([
      "/Users/z/Library/Application Support/Tutti-dev/agent-prompt-assets/room-1/ab/deadbeef.txt"
    ]);
  });

  it("rewrites persisted pasted-text instruction text into mention chips (reload-safe, preview label persists)", () => {
    const text =
      "Referenced pasted text files:\n" +
      '- pasted text file "first line": /home/u/agent-prompt-assets/r/ab/deadbeef.txt. Read this file before continuing.';
    // The localized header/instruction wording is dropped; the chip mention
    // keeps the quoted preview as its label and the path in the href — matching
    // the optimistic display.
    expect(linkifyPastedTextReferences(text)).toBe(
      "[@first line](mention://pasted-text/ref-0?path=%2Fhome%2Fu%2Fagent-prompt-assets%2Fr%2Fab%2Fdeadbeef.txt)"
    );
  });

  it("leaves text without pasted-text references unchanged", () => {
    expect(linkifyPastedTextReferences("just a normal message")).toBe(
      "just a normal message"
    );
  });
});
