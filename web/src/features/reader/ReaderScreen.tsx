import type { ReactNode } from "react";
import type { ReaderTheme } from "./readerSettings";

// Themed full-screen wrapper for the reader's loading, error, and empty
// states so every pre-engine screen shares the reader canvas.
export function ReaderScreen({
  theme,
  pad = false,
  children,
}: {
  theme: ReaderTheme;
  pad?: boolean;
  children: ReactNode;
}) {
  return (
    <div
      data-theme={theme}
      className={`reader grid min-h-screen place-items-center bg-(--reader-canvas) text-(--reader-text)${pad ? " p-5" : ""}`}
    >
      {children}
    </div>
  );
}
