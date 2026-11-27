import { afterEach, expect, it } from "vitest";
import { cleanup, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithI18n } from "../../../../test/i18n";
import { GraphText } from "./graph-record-text";
afterEach(cleanup);
it("does not add an excerpt or more-control to fewer than 1200 returned code points", () => {
  const content = "😀".repeat(800);
  renderWithI18n(<GraphText content={content} testId="short-astral"/>);
  expect(screen.getByTestId("short-astral")).toBeVisible();
  expect(screen.getByTestId("short-astral").textContent).toBe(content);
  expect(screen.queryByText("Read more")).not.toBeInTheDocument();
});
it("shows readable names without link or image requests or executable markup", () => {
  renderWithI18n(<GraphText content={'[@复核](mention://agent/fake) [文本](javascript:alert(1))<img src="https://foreign.invalid/img"><script>alert(1)</script>'}/>);
  expect(screen.getByText('@复核 文本<img src="https://foreign.invalid/img"><script>alert(1)</script>')).toBeVisible();
  expect(document.querySelector("a,img,script,[onerror]")).toBeNull();
});
it("keeps all long returned text reachable through a disclosure instead of a clipped stored fact", async () => {
  const content = "中😀".repeat(3000) + "结束";
  renderWithI18n(<GraphText content={content} testId="full-result"/>);
  const full = screen.getByTestId("full-result");
  expect(full.textContent).toBe(content);
  expect(full).not.toBeVisible();
  await userEvent.setup().click(screen.getByText("Read more"));
  expect(full).toBeVisible();
});

it("keeps a Unicode excerpt whole and makes an all-HTML record inspectably nonblank", () => {
  const text = "a".repeat(1199) + "😀" + "tail";
  renderWithI18n(<GraphText content={text}/>);
  expect(screen.getByText("a".repeat(1199) + "😀...")).toBeVisible();
  cleanup();
  renderWithI18n(<GraphText content="<script>hiddenCode()</script>"/>);
  expect(screen.getByText("<script>hiddenCode()</script>")).toBeVisible();
  expect(document.querySelector("script")).toBeNull();
});
