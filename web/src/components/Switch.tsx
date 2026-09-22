import type { ReactNode } from "react";

type SwitchProps = {
  checked: boolean;
  onChange: () => void;
  disabled?: boolean;
  className?: string;
  trackPosition?: "start" | "end";
  size?: "sm" | "md";
  children: ReactNode;
};

// Single anchored toggle. The knob stays in normal flow inside a padded track
// and docks with justify-start/justify-end, so it cannot drift past the track
// boundary in either state.
export function Switch({
  checked,
  onChange,
  disabled,
  className,
  trackPosition = "start",
  size = "sm",
  children,
}: SwitchProps) {
  const track = (
    <span
      aria-hidden="true"
      className={`flex shrink-0 items-center rounded-full border p-0.5 ${
        size === "md" ? "h-6 w-11" : "h-5 w-9"
      } ${
        checked
          ? "justify-end border-amber-400 bg-amber-400"
          : "justify-start border-zinc-700 bg-zinc-900"
      }`}
    >
      <span
        className={`shrink-0 rounded-full ${size === "md" ? "size-5" : "size-3.5"} ${
          checked ? "bg-zinc-950" : "bg-zinc-400"
        }`}
      />
    </span>
  );
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      disabled={disabled}
      onClick={onChange}
      className={className}
    >
      {trackPosition === "start" ? track : null}
      {children}
      {trackPosition === "end" ? track : null}
    </button>
  );
}
