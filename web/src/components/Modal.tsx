import { useEffect, useRef } from "react";
import { createPortal } from "react-dom";
import { X } from "lucide-react";
import type { ReactNode } from "react";

const FOCUSABLE = 'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])';

// Module-level count so nested dialogs keep the body locked until the last
// one unmounts.
let lockedCount = 0;
function lockBody() {
  lockedCount += 1;
  if (lockedCount === 1) {
    document.body.style.overflow = "hidden";
  }
}
function unlockBody() {
  lockedCount = Math.max(0, lockedCount - 1);
  if (lockedCount === 0) {
    document.body.style.overflow = "";
  }
}

const SWIPE_CLOSE_PX = 72;

export function Modal({
  title,
  children,
  onClose,
  variant = "center",
}: {
  title: string;
  children: ReactNode;
  onClose: () => void;
  variant?: "center" | "sheet";
}) {
  const panel = useRef<HTMLElement>(null);
  const closeRef = useRef(onClose);
  const touchStartY = useRef<number | null>(null);
  const touchDeltaY = useRef(0);
  useEffect(() => {
    closeRef.current = onClose;
  });
  // Focus moves into the dialog on mount and returns to the element that
  // opened it on unmount. This runs once: a changing onClose identity must
  // not steal focus back to the panel in the middle of an interaction.
  useEffect(() => {
    const restore = document.activeElement;
    panel.current?.focus();
    return () => {
      if (restore instanceof HTMLElement) restore.focus();
    };
  }, []);
  // Body scroll stays locked while any modal is open so the page behind a
  // bottom sheet cannot scroll.
  useEffect(() => {
    lockBody();
    return () => unlockBody();
  }, []);
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        closeRef.current();
        return;
      }
      if (event.key !== "Tab" || !panel.current) return;
      // aria-modal promises the rest of the page is inert, so Tab cycles
      // inside the dialog instead of escaping into the content behind it.
      const items = Array.from(panel.current.querySelectorAll<HTMLElement>(FOCUSABLE)).filter(
        (element) => !element.hasAttribute("disabled"),
      );
      if (items.length === 0) {
        event.preventDefault();
        return;
      }
      const first = items[0];
      const last = items[items.length - 1];
      const active = document.activeElement;
      if (event.shiftKey && (active === first || !panel.current.contains(active))) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && (active === last || !panel.current.contains(active))) {
        event.preventDefault();
        first.focus();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);
  const isSheet = variant === "sheet";
  const onTouchStart = (event: React.TouchEvent) => {
    if (!isSheet || event.touches.length !== 1) return;
    touchStartY.current = event.touches[0].clientY;
    touchDeltaY.current = 0;
  };
  const onTouchMove = (event: React.TouchEvent) => {
    if (!isSheet || touchStartY.current === null || event.touches.length !== 1) return;
    touchDeltaY.current = event.touches[0].clientY - touchStartY.current;
  };
  const onTouchEnd = () => {
    if (!isSheet || touchStartY.current === null) return;
    const delta = touchDeltaY.current;
    touchStartY.current = null;
    touchDeltaY.current = 0;
    if (delta > SWIPE_CLOSE_PX) closeRef.current();
  };
  return createPortal(
    <div
      className={
        isSheet
          ? "fixed inset-0 z-50 grid items-end bg-black/70 sm:place-items-center sm:p-4"
          : "fixed inset-0 z-50 grid place-items-center bg-black/70 p-4"
      }
      onMouseDown={onClose}
    >
      <section
        ref={panel}
        tabIndex={-1}
        role="dialog"
        aria-modal="true"
        aria-label={title}
        onTouchStart={onTouchStart}
        onTouchMove={onTouchMove}
        onTouchEnd={onTouchEnd}
        className={
          isSheet
            ? "sheet-panel max-h-[90dvh] w-full overflow-y-auto rounded-t-2xl border border-zinc-700 bg-zinc-900 p-5 pb-[calc(1.25rem+env(safe-area-inset-bottom,0px))] shadow-2xl outline-none sm:max-h-[90vh] sm:max-w-2xl sm:rounded-2xl"
            : "max-h-[90vh] w-full max-w-2xl overflow-y-auto rounded-2xl border border-zinc-700 bg-zinc-900 p-5 shadow-2xl outline-none"
        }
        onMouseDown={(event) => event.stopPropagation()}
      >
        {isSheet && (
          <div
            aria-hidden="true"
            className="mx-auto mb-3 h-1 w-10 rounded-full bg-zinc-700 sm:hidden"
          />
        )}
        <div className="mb-5 flex items-center justify-between">
          <h2 className="text-xl font-semibold">{title}</h2>
          <button
            aria-label="Close"
            onClick={onClose}
            className="rounded-lg p-2 text-zinc-400 hover:bg-zinc-800 hover:text-white active:bg-zinc-800 active:text-white"
          >
            <X size={17} />
          </button>
        </div>
        {children}
      </section>
    </div>,
    document.body,
  );
}
