import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vite-plus/test";
import { Switch } from "./Switch";

describe("Switch", () => {
  it("docks the knob left when off and right when on", () => {
    const { rerender } = render(
      <Switch checked={false} onChange={() => {}}>
        Auto download
      </Switch>,
    );
    const toggle = screen.getByRole("switch", { name: "Auto download" });
    expect(toggle).toHaveAttribute("aria-checked", "false");
    expect(toggle.firstElementChild).toHaveClass("justify-start");

    rerender(
      <Switch checked={true} onChange={() => {}}>
        Auto download
      </Switch>,
    );
    expect(toggle).toHaveAttribute("aria-checked", "true");
    expect(toggle.firstElementChild).toHaveClass("justify-end");
  });

  it("dispatches the change handler and respects disabled", () => {
    const onChange = vi.fn();
    const { rerender } = render(
      <Switch checked={false} onChange={onChange}>
        Auto download
      </Switch>,
    );
    fireEvent.click(screen.getByRole("switch", { name: "Auto download" }));
    expect(onChange).toHaveBeenCalledTimes(1);

    rerender(
      <Switch checked={false} onChange={onChange} disabled>
        Auto download
      </Switch>,
    );
    fireEvent.click(screen.getByRole("switch", { name: "Auto download" }));
    expect(onChange).toHaveBeenCalledTimes(1);
  });

  it("places the track after the label when trackPosition is end", () => {
    render(
      <Switch checked={false} onChange={() => {}} trackPosition="end">
        Auto download
      </Switch>,
    );
    const toggle = screen.getByRole("switch", { name: "Auto download" });
    expect(toggle.lastElementChild).toHaveClass("justify-start");
  });
});
