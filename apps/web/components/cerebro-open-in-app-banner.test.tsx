import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { CerebroOpenInAppBanner } from "./cerebro-open-in-app-banner";

const IPHONE_UA =
  "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1";
const MAC_UA =
  "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15";

function setUserAgent(ua: string) {
  Object.defineProperty(navigator, "userAgent", {
    value: ua,
    configurable: true,
  });
}

function setStandalone(isStandalone: boolean) {
  Object.defineProperty(window.navigator, "standalone", {
    value: isStandalone,
    configurable: true,
  });
  // matchMedia is a real function; stub to mirror standalone for the
  // display-mode query.
  window.matchMedia = vi.fn().mockImplementation((q: string) => ({
    matches: q.includes("standalone") ? isStandalone : false,
    media: q,
    onchange: null,
    addEventListener: () => {},
    removeEventListener: () => {},
    addListener: () => {},
    removeListener: () => {},
    dispatchEvent: () => false,
  })) as unknown as typeof window.matchMedia;
}

describe("<CerebroOpenInAppBanner />", () => {
  beforeEach(() => {
    window.localStorage.clear();
    setStandalone(false);
  });

  afterEach(() => {
    setUserAgent(MAC_UA);
  });

  it("renders nothing on a desktop user agent", () => {
    setUserAgent(MAC_UA);
    const { container } = render(<CerebroOpenInAppBanner />);
    expect(container).toBeEmptyDOMElement();
  });

  it("renders on iPhone Safari with default copy", () => {
    setUserAgent(IPHONE_UA);
    render(<CerebroOpenInAppBanner />);
    expect(screen.getByTestId("open-in-app-banner")).toBeInTheDocument();
    expect(screen.getByText("Multica")).toBeInTheDocument();
    expect(screen.getByText("Open this page in the app")).toBeInTheDocument();
    const open = screen.getByTestId("open-in-app-banner-open");
    // Default href is current pathname; jsdom defaults to "/".
    expect(open).toHaveAttribute("href", "/");
  });

  it("renders nothing when running as a standalone PWA on iPhone", () => {
    setUserAgent(IPHONE_UA);
    setStandalone(true);
    const { container } = render(<CerebroOpenInAppBanner />);
    expect(container).toBeEmptyDOMElement();
  });

  it("uses the supplied targetUrl on the open link", () => {
    setUserAgent(IPHONE_UA);
    render(<CerebroOpenInAppBanner targetUrl="/issues/MUL-42" />);
    expect(screen.getByTestId("open-in-app-banner-open")).toHaveAttribute(
      "href",
      "/issues/MUL-42",
    );
  });

  it("respects custom title / description / labels", () => {
    setUserAgent(IPHONE_UA);
    render(
      <CerebroOpenInAppBanner
        title="Custom Title"
        description="Custom description"
        openLabel="Launch"
        closeLabel="Close"
      />,
    );
    expect(screen.getByText("Custom Title")).toBeInTheDocument();
    expect(screen.getByText("Custom description")).toBeInTheDocument();
    expect(screen.getByTestId("open-in-app-banner-open")).toHaveTextContent(
      "Launch",
    );
    expect(screen.getByTestId("open-in-app-banner-dismiss")).toHaveAccessibleName(
      "Close",
    );
  });

  it("hides the close button when closeable=false", () => {
    setUserAgent(IPHONE_UA);
    render(<CerebroOpenInAppBanner closeable={false} />);
    expect(screen.getByTestId("open-in-app-banner")).toBeInTheDocument();
    expect(
      screen.queryByTestId("open-in-app-banner-dismiss"),
    ).not.toBeInTheDocument();
  });

  it("dismiss hides the banner and persists the timestamp", async () => {
    setUserAgent(IPHONE_UA);
    const user = userEvent.setup();
    const { rerender } = render(<CerebroOpenInAppBanner />);
    await user.click(screen.getByTestId("open-in-app-banner-dismiss"));
    expect(
      screen.queryByTestId("open-in-app-banner"),
    ).not.toBeInTheDocument();

    // After dismiss the localStorage flag is set; re-render should not
    // resurrect the banner within the dismiss window.
    rerender(<CerebroOpenInAppBanner />);
    expect(
      screen.queryByTestId("open-in-app-banner"),
    ).not.toBeInTheDocument();

    expect(
      window.localStorage.getItem("multica:open_in_app:dismissed_at"),
    ).not.toBeNull();
  });

  it("namespaces dismiss state per storageKey so two banners stay independent", async () => {
    setUserAgent(IPHONE_UA);
    const user = userEvent.setup();
    render(
      <>
        <CerebroOpenInAppBanner storageKey="page-a" title="A" />
      </>,
    );
    await user.click(screen.getByTestId("open-in-app-banner-dismiss"));

    // A different storageKey instance still shows.
    await act(async () => {
      // ensure microtasks settle
    });
    const { container } = render(
      <CerebroOpenInAppBanner storageKey="page-b" title="B" />,
    );
    expect(container.querySelector('[data-testid="open-in-app-banner"]')).toBeTruthy();
  });

  it("exposes a labelled region for assistive tech", () => {
    setUserAgent(IPHONE_UA);
    render(<CerebroOpenInAppBanner />);
    expect(
      screen.getByRole("region", { name: "Open in app" }),
    ).toBeInTheDocument();
  });
});
