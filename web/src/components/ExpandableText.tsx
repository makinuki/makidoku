import { useEffect, useRef, useState } from "react";

// lineClamp maps the supported clamp sizes to literal classes so the
// utility scanner picks them up.
const lineClamp: Record<number, string> = {
  1: "line-clamp-1",
  2: "line-clamp-2",
  3: "line-clamp-3",
  4: "line-clamp-4",
};

// ExpandableText clamps long copy to a fixed line count and offers a toggle
// only when the content actually overflows the clamp.
export function ExpandableText({
  text,
  lines = 3,
  className = "",
}: {
  text: string;
  lines?: number;
  className?: string;
}) {
  const [expanded, setExpanded] = useState(false);
  const [overflows, setOverflows] = useState(false);
  const ref = useRef<HTMLParagraphElement>(null);

  useEffect(() => {
    setExpanded(false);
  }, [text]);

  useEffect(() => {
    const element = ref.current;
    if (!element || expanded) return;
    const measure = () => setOverflows(element.scrollHeight > element.clientHeight + 1);
    measure();
    window.addEventListener("resize", measure);
    return () => window.removeEventListener("resize", measure);
  }, [text, lines, expanded]);

  return (
    <div>
      <p
        ref={ref}
        className={`${expanded ? "" : (lineClamp[lines] ?? "line-clamp-3")} ${className}`}
      >
        {text}
      </p>
      {(overflows || expanded) && (
        <button
          type="button"
          onClick={() => setExpanded((value) => !value)}
          className="mt-1 text-xs font-medium text-amber-300 hover:text-amber-200"
        >
          {expanded ? "Show less" : "Show more"}
        </button>
      )}
    </div>
  );
}
