// @vitest-environment jsdom

import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enRuntimes from "../../locales/en/runtimes.json";
import { ResumeWarningNotice } from "./runtime-detail";
import { readRuntimeResumeWarning } from "../runtime-resume-warning";

const TEST_RESOURCES = { en: { common: enCommon, runtimes: enRuntimes } };

const VALID_WARNING = {
	code: "prior_session_resume_unavailable" as const,
	task_id: "11111111-2222-3333-4444-555555555555",
	occurred_at: "2026-09-19T00:00:00Z",
};

describe("runtime resume warning metadata", () => {
	it("parses the daemon entry", () => {
		expect(readRuntimeResumeWarning({ resume_warning: VALID_WARNING })).toEqual(
			VALID_WARNING,
		);
	});

	it("fails closed on absent or malformed metadata", () => {
		const malformed = [
			undefined,
			null,
			{},
			"nope",
			{ resume_warning: null },
			{ resume_warning: "prior_session_resume_unavailable" },
			{ resume_warning: { code: "some_other_code", task_id: "t", occurred_at: "t" } },
			{ resume_warning: { code: "prior_session_resume_unavailable" } },
			{
				resume_warning: {
					code: "prior_session_resume_unavailable",
					task_id: "  ",
					occurred_at: "2026-09-19T00:00:00Z",
				},
			},
			{
				resume_warning: {
					code: "prior_session_resume_unavailable",
					task_id: "t",
				},
			},
		];
		for (const metadata of malformed) {
			expect(readRuntimeResumeWarning(metadata)).toBeNull();
		}
	});
});

describe("ResumeWarningNotice", () => {
	it("shows the continuity warning for a valid entry", () => {
		render(
			<I18nProvider locale="en" resources={TEST_RESOURCES}>
				<ResumeWarningNotice warning={VALID_WARNING} />
			</I18nProvider>,
		);
		expect(screen.getByTestId("runtime-resume-warning")).toBeTruthy();
		expect(screen.getByText("Session continuity")).toBeTruthy();
		expect(
			screen.getByText(/A previous session could not be restored/),
		).toBeTruthy();
	});

	it("renders no notice when the metadata is malformed", () => {
		const warning = readRuntimeResumeWarning({
			resume_warning: { code: "prior_session_resume_unavailable" },
		});
		expect(warning).toBeNull();
		const { container } = render(
			<I18nProvider locale="en" resources={TEST_RESOURCES}>
				<div>{warning && <ResumeWarningNotice warning={warning} />}</div>
			</I18nProvider>,
		);
		expect(container.querySelector('[data-testid="runtime-resume-warning"]')).toBeNull();
	});
});

