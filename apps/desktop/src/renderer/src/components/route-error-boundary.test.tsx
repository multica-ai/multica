import type { ReactNode } from "react";
import { render, screen } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { describe, expect, it } from "vitest";
import { NotFoundPage, RouteErrorBoundary } from "./route-error-boundary";

function renderWithRouter(path: string, element: ReactNode) {
  const router = createMemoryRouter(
    [
      {
        path: "/:workspaceSlug/*",
        element,
      },
    ],
    { initialEntries: [path] },
  );

  return render(<RouterProvider router={router} />);
}

describe("NotFoundPage", () => {
  it("renders a custom 404 message instead of React Router's default developer copy", () => {
    renderWithRouter("/acme/missing-route", <NotFoundPage />);

    expect(screen.getByRole("heading", { name: "Page not found" })).toBeInTheDocument();
    expect(screen.getByText("Error 404")).toBeInTheDocument();
    expect(screen.getByText("/acme/missing-route")).toBeInTheDocument();
    expect(screen.queryByText(/Hey developer/i)).not.toBeInTheDocument();
  });
});

describe("RouteErrorBoundary", () => {
  it("renders a friendly error state for thrown route errors", () => {
    const router = createMemoryRouter(
      [
        {
          path: "/broken",
          element: <div />,
          errorElement: <RouteErrorBoundary />,
          loader: () => {
            throw new Response("Not found", { status: 404, statusText: "Not Found" });
          },
        },
      ],
      { initialEntries: ["/broken"] },
    );

    render(<RouterProvider router={router} />);

    expect(screen.getByRole("heading", { name: "Page not found" })).toBeInTheDocument();
    expect(screen.getByText("Error 404")).toBeInTheDocument();
    expect(screen.queryByText(/Unexpected Application Error/i)).not.toBeInTheDocument();
  });
});
