import { X } from "lucide-react";

interface NoticeProps {
  message: string;
  onDismiss: () => void;
  actionLabel?: string;
  onAction?: () => void;
  className?: string;
}

// Non-modal status notice with an optional primary action and a dismiss
// control. Positioning is left to the caller through className, so the same
// component serves inline notices and floating banners.
export function Notice({ message, onDismiss, actionLabel, onAction, className }: NoticeProps) {
  return (
    <div
      role="status"
      className={`flex items-center justify-between gap-3 rounded-lg border border-amber-500/40 bg-zinc-900 px-3 py-2 text-xs text-amber-200 shadow-lg shadow-black/50 ${className ?? ""}`}
    >
      <span>{message}</span>
      <span className="flex shrink-0 items-center gap-1">
        {actionLabel && onAction && (
          <button
            onClick={onAction}
            className="rounded-md border border-amber-400/40 px-2 py-1 font-semibold text-amber-100 hover:bg-amber-500/20"
          >
            {actionLabel}
          </button>
        )}
        <button
          onClick={onDismiss}
          className="p-1 text-amber-200/70 hover:text-amber-100"
          aria-label="Dismiss notice"
        >
          <X size={14} />
        </button>
      </span>
    </div>
  );
}
