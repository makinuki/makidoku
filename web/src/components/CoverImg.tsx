import { Image as ImageIcon } from "lucide-react";
import { useState } from "react";

// CoverImg renders a title cover and swaps to a labelled placeholder when the
// source provides no artwork or the image request fails.
export function CoverImg({ src, className }: { src?: string; className?: string }) {
  const [failed, setFailed] = useState(false);
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
    <img src={src} alt="" loading="lazy" onError={() => setFailed(true)} className={className} />
  );
}
