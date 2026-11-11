import { describe, expect, it } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Users } from "@multica/ui/icons";
import { renderWithI18n } from "../test/i18n";
import { FeatureIntro, FeatureIntroCarousel } from "./feature-intro";

const SLIDES = [
  { id: "one", title: "First idea", description: "First body", art: <div>art one</div> },
  { id: "two", title: "Second idea", description: "Second body", art: <div>art two</div> },
  { id: "three", title: "Third idea", description: "Third body", art: <div>art three</div> },
];

function renderIntro(locale: "en" | "zh-Hans" = "en") {
  return renderWithI18n(
    <FeatureIntro
      icon={Users}
      label="Squads"
      title="Work as a team"
      description="Explains the feature."
      action={<button type="button">New squad</button>}
      docs="squads"
      columns={["Name", "Leader"]}
    >
      <FeatureIntroCarousel label="Work as a team" slides={SLIDES} />
    </FeatureIntro>,
    { locale },
  );
}

function slide(name: string) {
  return screen.getByText(name).closest("[aria-roledescription='slide']")!;
}

describe("FeatureIntro", () => {
  it("names the section by its headline and offers create plus docs", () => {
    renderIntro();

    expect(screen.getByRole("region", { name: "Work as a team" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { level: 2, name: "Work as a team" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "New squad" })).toBeInTheDocument();
    const docs = screen.getByRole("link", { name: "Read the docs" });
    expect(docs).toHaveAttribute("href", "https://multica.ai/docs/squads");
    expect(docs).toHaveAttribute("target", "_blank");
  });

  it("links to the docs in the reader's language", () => {
    renderIntro("zh-Hans");
    expect(screen.getByRole("link", { name: "阅读文档" })).toHaveAttribute(
      "href",
      "https://multica.ai/docs/zh/squads",
    );
  });

  it("keeps the faded list out of the accessibility tree", () => {
    renderIntro();
    expect(screen.getByTestId("feature-intro-backdrop")).toHaveAttribute("aria-hidden", "true");
  });

  it("pages through slides with the arrows and dots", async () => {
    const user = userEvent.setup();
    renderIntro();

    const previous = screen.getByRole("button", { name: "Previous" });
    const next = screen.getByRole("button", { name: "Next" });
    expect(previous).toBeDisabled();
    expect(slide("First idea")).not.toHaveAttribute("aria-hidden", "true");
    expect(slide("Second idea")).toHaveAttribute("aria-hidden", "true");

    await user.click(next);
    expect(slide("First idea")).toHaveAttribute("aria-hidden", "true");
    expect(slide("Second idea")).not.toHaveAttribute("aria-hidden", "true");
    expect(previous).toBeEnabled();

    await user.click(screen.getByRole("button", { name: "Go to slide 3" }));
    expect(slide("Third idea")).not.toHaveAttribute("aria-hidden", "true");
    expect(next).toBeDisabled();
  });

  it("moves between slides with the arrow keys", () => {
    renderIntro();
    const carousel = screen.getByRole("region", { name: "Work as a team" }).querySelector(
      "[aria-roledescription='carousel']",
    )!;

    fireEvent.keyDown(carousel, { key: "ArrowRight" });
    expect(slide("Second idea")).not.toHaveAttribute("aria-hidden", "true");
    fireEvent.keyDown(carousel, { key: "ArrowLeft" });
    fireEvent.keyDown(carousel, { key: "ArrowLeft" });
    expect(slide("First idea")).not.toHaveAttribute("aria-hidden", "true");
  });
});
