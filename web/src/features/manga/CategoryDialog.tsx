import { useState } from "react";
import { Modal } from "../../components/Modal";
import type { Category } from "../../types";

type CategoryDialogProps = {
  categories: Category[];
  initialSelected: number[];
  showRemove: boolean;
  busy: boolean;
  removing: boolean;
  onApply: (selectedIds: number[]) => void;
  onRemove: () => void;
  onEditCategories: () => void;
  onClose: () => void;
};

// Category picker shown from the library button. Mirrors the reference
// reader-app flow: checkbox list with the current assignment pre-checked,
// an Edit shortcut to the category manager, and OK to apply the diff.
export function CategoryDialog({
  categories,
  initialSelected,
  showRemove,
  busy,
  removing,
  onApply,
  onRemove,
  onEditCategories,
  onClose,
}: CategoryDialogProps) {
  const [selected, setSelected] = useState<number[]>(initialSelected);
  const locked = busy || removing;
  const toggle = (id: number) =>
    setSelected((items) =>
      items.includes(id) ? items.filter((item) => item !== id) : [...items, id],
    );
  return (
    <Modal title="Set categories" onClose={onClose}>
      <div className="max-h-[50vh] overflow-y-auto">
        {categories.map((category) => (
          <label
            key={category.id}
            className="flex min-h-11 cursor-pointer items-center gap-3 px-1 py-2 text-sm hover:bg-zinc-800/50"
          >
            <input
              type="checkbox"
              checked={selected.includes(category.id)}
              onChange={() => toggle(category.id)}
              disabled={locked}
              className="size-5 shrink-0 accent-amber-400 disabled:opacity-50"
            />
            <span className="min-w-0 flex-1 truncate">{category.name}</span>
          </label>
        ))}
      </div>
      {showRemove && (
        <button
          type="button"
          onClick={onRemove}
          disabled={locked}
          className="mt-2 w-full rounded-lg border border-red-900/60 px-4 py-2.5 text-sm text-red-300 hover:bg-red-950/40 disabled:opacity-50"
        >
          {removing ? "Removing…" : "Remove from library"}
        </button>
      )}
      <div className="mt-3 flex items-center gap-2">
        <button
          type="button"
          onClick={onEditCategories}
          disabled={locked}
          className="rounded-lg px-4 py-2.5 text-sm text-zinc-300 hover:bg-zinc-800 disabled:opacity-50"
        >
          Edit
        </button>
        <span className="flex-1" />
        <button
          type="button"
          onClick={onClose}
          disabled={locked}
          className="rounded-lg px-4 py-2.5 text-sm text-zinc-300 hover:bg-zinc-800 disabled:opacity-50"
        >
          Cancel
        </button>
        <button
          type="button"
          onClick={() => onApply(selected)}
          disabled={locked}
          className="rounded-lg bg-amber-400 px-4 py-2.5 text-sm font-semibold text-zinc-950 hover:bg-amber-300 disabled:opacity-50"
        >
          {busy ? "Saving…" : "OK"}
        </button>
      </div>
    </Modal>
  );
}
