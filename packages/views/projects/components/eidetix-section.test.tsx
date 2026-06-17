import { render, screen, fireEvent } from "@testing-library/react";
import { describe, expect, it, vi, beforeEach } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enProjects from "../../locales/en/projects.json";

const TEST_RESOURCES = { en: { common: enCommon, projects: enProjects } };

const setMutate = vi.fn();
const toggleMutate = vi.fn();
const clearMutate = vi.fn();
let mockRole = "owner";
let mockData = {
  configured: true,
  enabled: true,
  endpoint_url: "",
  graph_label: "Marketing",
};

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws1" }));
vi.mock("@multica/core/permissions", () => ({
  useCurrentMember: () => ({ role: mockRole }),
}));
vi.mock("@multica/core/projects", () => ({
  projectEidetixOptions: () => ({ queryKey: ["e"], queryFn: vi.fn() }),
  useSetProjectEidetix: () => ({ mutate: setMutate, isPending: false }),
  useToggleProjectEidetix: () => ({ mutate: toggleMutate, isPending: false }),
  useClearProjectEidetix: () => ({ mutate: clearMutate, isPending: false }),
}));
vi.mock("@tanstack/react-query", () => ({ useQuery: () => ({ data: mockData }) }));

import { ProjectEidetixSection } from "./eidetix-section";

function renderSection(projectId = "p1") {
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <ProjectEidetixSection projectId={projectId} />
    </I18nProvider>,
  );
}

beforeEach(() => {
  setMutate.mockReset();
  toggleMutate.mockReset();
  clearMutate.mockReset();
  mockRole = "owner";
  mockData = {
    configured: true,
    enabled: true,
    endpoint_url: "",
    graph_label: "Marketing",
  };
});

describe("ProjectEidetixSection", () => {
  it("renders nothing for a non-admin member", () => {
    mockRole = "member";
    const { container } = renderSection();
    expect(container).toBeEmptyDOMElement();
  });

  it("shows configured status + graph label for an admin", () => {
    renderSection();
    const header = screen.getByRole("button", { name: /eidetix/i });
    fireEvent.click(header);
    expect(screen.getByText(/Marketing/)).toBeInTheDocument();
  });

  it("toggles enabled", () => {
    renderSection();
    fireEvent.click(screen.getByRole("button", { name: /eidetix/i }));
    fireEvent.click(screen.getByRole("button", { name: /disable/i }));
    expect(toggleMutate).toHaveBeenCalledWith(false);
  });

  it("submits a new token; existing token never rendered", () => {
    mockData = {
      configured: false,
      enabled: false,
      endpoint_url: "",
      graph_label: "",
    };
    renderSection();
    fireEvent.click(screen.getByRole("button", { name: /eidetix/i }));
    fireEvent.change(screen.getByLabelText(/bearer token/i), {
      target: { value: "tok-123" },
    });
    fireEvent.click(screen.getByRole("button", { name: /set token/i }));
    expect(setMutate).toHaveBeenCalledWith(
      expect.objectContaining({ token: "tok-123" }),
      expect.anything(),
    );
  });
});
