import { useState } from "react";
import { api } from "../api";
import type { Source } from "../types";

// SourceIcon renders a plugin icon, falling back to its initials when the icon
// is missing or fails to load.
export function SourceIcon({ source, className }: { source: Source; className?: string }) {
  const [failed, setFailed] = useState(false);
  const classes = className ?? "size-9 shrink-0 rounded-lg";
  return source.iconUrl && !failed ? (
    <img
      src={api.sourceIcon(source.id)}
      alt=""
      onError={() => setFailed(true)}
      className={`${classes} object-cover`}
    />
  ) : (
    <span className={`${classes} grid place-items-center bg-zinc-800 text-xs font-bold`}>
      {source.name.slice(0, 2).toUpperCase()}
    </span>
  );
}
