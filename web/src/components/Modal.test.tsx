import { useState } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vite-plus/test";
import { Modal } from "./Modal";

function Harness() {
  const [open, setOpen] = useState(false);
  return (
    <div>
      <button onClick={() => setOpen(true)}>Open dialog</button>
      {open && (
        <Modal title="Details" onClose={() => setOpen(false)}>
          <button>First action</button>
          <button>Second action</button>
        </Modal>
      )}
    </div>
  );
}

function SheetHarness() {
  const [open, setOpen] = useState(false);
  return (
    <div>
      <button onClick={() => setOpen(true)}>Open sheet</button>
      {open && (
        <Modal title="Filters" variant="sheet" onClose={() => setOpen(false)}>
          <button>Apply</button>
        </Modal>
      )}
    </div>
  );
}
describe("Modal", () => {
  it("traps Tab inside the dialog and restores focus on close", () => {
    render(<Harness />);
    const trigger = screen.getByRole("button", { name: "Open dialog" });
    trigger.focus();
    fireEvent.click(trigger);
    const dialog = screen.getByRole("dialog", { name: "Details" });
    expect(dialog).toHaveFocus();
    const close = screen.getByRole("button", { name: "Close" });
    const first = screen.getByRole("button", { name: "First action" });
    const second = screen.getByRole("button", { name: "Second action" });
    // Forward from the last focusable wraps to the first.
    second.focus();
    fireEvent.keyDown(window, { key: "Tab" });
    expect(close).toHaveFocus();
    // Backward from the first wraps to the last.
    close.focus();
    fireEvent.keyDown(window, { key: "Tab", shiftKey: true });
    expect(second).toHaveFocus();
    expect(first).not.toHaveFocus();
    // Escape closes and focus returns to the element that opened the dialog.
    fireEvent.keyDown(window, { key: "Escape" });
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(trigger).toHaveFocus();
  });

  it("sheet variant locks body scroll and closes on swipe down", () => {
    render(<SheetHarness />);
    const trigger = screen.getByRole("button", { name: "Open sheet" });
    trigger.focus();
    fireEvent.click(trigger);
    const sheet = screen.getByRole("dialog", { name: "Filters" });
    expect(sheet).toHaveClass("sheet-panel");
    expect(document.body.style.overflow).toBe("hidden");
    // Short drag does not dismiss.
    fireEvent.touchStart(sheet, { touches: [{ clientY: 200 }] });
    fireEvent.touchMove(sheet, { touches: [{ clientY: 230 }] });
    fireEvent.touchEnd(sheet);
    expect(screen.getByRole("dialog", { name: "Filters" })).toBeInTheDocument();
    // Long drag past the threshold dismisses and restores scroll + focus.
    fireEvent.touchStart(sheet, { touches: [{ clientY: 200 }] });
    fireEvent.touchMove(sheet, { touches: [{ clientY: 300 }] });
    fireEvent.touchEnd(sheet);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(document.body.style.overflow).toBe("");
    expect(trigger).toHaveFocus();
  });
});
