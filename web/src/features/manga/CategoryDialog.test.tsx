import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vite-plus/test";
import { CategoryDialog } from "./CategoryDialog";
import type { Category } from "../../types";

const categories: Category[] = [
  { id: 1, name: "manga", sortOrder: 0 },
  { id: 2, name: "manhwa", sortOrder: 1 },
];

function renderDialog(overrides: Partial<Parameters<typeof CategoryDialog>[0]> = {}) {
  const handlers = {
    onApply: vi.fn(),
    onRemove: vi.fn(),
    onEditCategories: vi.fn(),
    onClose: vi.fn(),
    ...overrides,
  };
  render(
    <CategoryDialog
      categories={categories}
      initialSelected={[1]}
      showRemove={false}
      busy={false}
      removing={false}
      {...handlers}
    />,
  );
  return handlers;
}

describe("CategoryDialog", () => {
  it("pre-checks the current assignment and applies the edited selection", () => {
    const { onApply, onClose } = renderDialog();
    expect(screen.getByRole("checkbox", { name: "manga" })).toBeChecked();
    expect(screen.getByRole("checkbox", { name: "manhwa" })).not.toBeChecked();
    fireEvent.click(screen.getByRole("checkbox", { name: "manga" }));
    fireEvent.click(screen.getByRole("checkbox", { name: "manhwa" }));
    fireEvent.click(screen.getByRole("button", { name: "OK" }));
    expect(onApply).toHaveBeenCalledWith([2]);
    expect(onClose).not.toHaveBeenCalled();
  });

  it("applies nothing on cancel", () => {
    const { onApply, onClose } = renderDialog();
    fireEvent.click(screen.getByRole("checkbox", { name: "manhwa" }));
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(onApply).not.toHaveBeenCalled();
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("offers removal only in edit mode", () => {
    renderDialog({ showRemove: false });
    expect(screen.queryByRole("button", { name: "Remove from library" })).not.toBeInTheDocument();
  });

  it("calls remove in edit mode", () => {
    const { onRemove } = renderDialog({ showRemove: true });
    fireEvent.click(screen.getByRole("button", { name: "Remove from library" }));
    expect(onRemove).toHaveBeenCalledTimes(1);
  });

  it("disables actions while busy", () => {
    renderDialog({ busy: true });
    expect(screen.getByRole("button", { name: "Saving…" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Cancel" })).toBeDisabled();
    expect(screen.getByRole("checkbox", { name: "manga" })).toBeDisabled();
  });

  it("opens the category manager from Edit", () => {
    const { onEditCategories } = renderDialog();
    fireEvent.click(screen.getByRole("button", { name: "Edit" }));
    expect(onEditCategories).toHaveBeenCalledTimes(1);
  });
});
