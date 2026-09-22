import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vite-plus/test";
import { ExpandableText } from "./ExpandableText";

// jsdom reports zero element sizes, so overflow is simulated by defining the
// layout properties the component measures and re-running the resize pass.
function mockOverflow(element: HTMLElement, scrollHeight: number, clientHeight: number) {
  Object.defineProperty(element, "scrollHeight", { value: scrollHeight, configurable: true });
  Object.defineProperty(element, "clientHeight", { value: clientHeight, configurable: true });
  fireEvent(window, new Event("resize"));
}

describe("ExpandableText", () => {
  it("renders the full text and hides the toggle when nothing overflows", () => {
    const { container } = render(<ExpandableText text="Short description" lines={3} />);
    expect(screen.getByText("Short description")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Show more" })).not.toBeInTheDocument();
    expect(container.querySelector("p")).toHaveClass("line-clamp-3");
  });

  it("offers a toggle when the text overflows and expands on click", () => {
    const { container } = render(<ExpandableText text={"Long text ".repeat(50)} lines={2} />);
    const paragraph = container.querySelector("p")!;
    mockOverflow(paragraph, 120, 40);
    expect(screen.getByRole("button", { name: "Show more" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Show more" }));
    expect(screen.getByRole("button", { name: "Show less" })).toBeInTheDocument();
    expect(paragraph).not.toHaveClass("line-clamp-2");
    fireEvent.click(screen.getByRole("button", { name: "Show less" }));
    expect(paragraph).toHaveClass("line-clamp-2");
  });
});
