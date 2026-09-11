import { Image as ImageIcon } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { acquireCoverSlot, type CoverSlot } from "./coverQueue";

// CoverImg renders a title cover and swaps to a labelled placeholder when the
// source provides no artwork or the image request fails.
//
// The request is deferred twice: until the element is near the viewport, and
// until the cover queue has a free slot. A long grid of covers otherwise
// occupies every browser connection at once and blocks the rest of the app.
export function CoverImg({ src, className }: { src?: string; className?: string }) {
  const [failed, setFailed] = useState(false);
  const [nearViewport, setNearViewport] = useState(false);
  const [allowed, setAllowed] = useState(false);
  const node = useRef<HTMLImageElement | null>(null);
  const slot = useRef<CoverSlot | null>(null);

  useEffect(() => {
    setFailed(false);
    setNearViewport(false);
    setAllowed(false);
  }, [src]);

  useEffect(() => {
    if (!src || failed) return;
    const element = node.current;
    // Without an observer (older webviews and the test environment) the cover
    // loads immediately and only the queue bounds it.
    if (!element || typeof IntersectionObserver === "undefined") {
      setNearViewport(true);
      return;
    }
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting)) {
          setNearViewport(true);
          observer.disconnect();
        }
      },
      { rootMargin: "200px" },
    );
    observer.observe(element);
    return () => observer.disconnect();
  }, [src, failed]);

  useEffect(() => {
    if (!src || failed || !nearViewport) return;
    const handle = acquireCoverSlot();
    slot.current = handle;
    let mounted = true;
    void handle.ready.then(() => {
      if (mounted) setAllowed(true);
    });
    return () => {
      mounted = false;
      handle.cancel();
      if (slot.current === handle) slot.current = null;
    };
    // allowed is deliberately absent: granting a slot must not re-run this
    // effect, because the cleanup would hand the slot straight back and the
    // next cover would start before this one finished.
  }, [src, failed, nearViewport]);

  const finish = () => {
    slot.current?.release();
    slot.current = null;
  };

  if (!src || failed) {
    return (
      <div
        role="img"
        aria-label="No cover available"
        className={`grid place-items-center bg-zinc-800 text-zinc-600 ${className ?? ""}`}
      >
        <ImageIcon size={28} />
      </div>
    );
  }
  return (
    <img
      ref={node}
      src={allowed ? src : undefined}
      alt=""
      loading="lazy"
      decoding="async"
      onLoad={finish}
      onError={() => {
        finish();
        setFailed(true);
      }}
      className={className}
    />
  );
}
