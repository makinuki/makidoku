// Host page body for the prebuilt views: the daemon image with the reader fit
// styles, image filters, and an inline retry. Load outcomes report straight
// into the engine failure registry, which the retry pill reads; the retry
// button itself stays per mount, so a page that remounts after a spread round
// trip loads again instead of sticking on its earlier failure.

import { memo, useEffect, useState } from "react";
import type { CSSProperties } from "react";
import { IMAGE_LOAD_FAILED, type MekuriEngine } from "@makinuki/mekuri/engine";
import { api } from "../../api";
import type { Page } from "../../types";

export const HostPageImage = memo(function HostPageImage({
  page,
  alt,
  className,
  style,
  loading,
  priority,
  attempt,
  engine,
  onRetry,
}: {
  page: Page;
  alt: string;
  className: string;
  style?: CSSProperties;
  loading?: "lazy" | "eager";
  priority?: boolean;
  attempt: number;
  engine: MekuriEngine;
  onRetry: (id: string) => void;
}) {
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    if (attempt > 0) setFailed(false);
  }, [attempt]);
  if (failed)
    return (
      <button
        onClick={() => onRetry(page.id)}
        className={`grid h-64 w-full place-items-center rounded-sm border border-zinc-800 bg-zinc-900 text-sm text-zinc-300 ${className}`}
      >
        Retry page
      </button>
    );
  return (
    <img
      src={`${api.readerImage(page)}${attempt ? `?retry=${attempt}` : ""}`}
      alt={alt}
      onError={() => {
        setFailed(true);
        engine.reportPageLoadFailed(
          page.id,
          IMAGE_LOAD_FAILED,
          `Page ${page.index + 1} reported a load error`,
        );
      }}
      onLoad={() => engine.reportPageLoaded(page.id)}
      className={className}
      style={style}
      loading={loading ?? (priority ? "eager" : undefined)}
      decoding="async"
      fetchPriority={priority ? "high" : "auto"}
    />
  );
});
