import { useCallback, useEffect, useState } from "react";
import { X } from "lucide-react";
import { api } from "../../../api";
import type { Category } from "../../../types";
import { Modal } from "../../../components/Modal";
import { useSettings } from "../SettingsLayout";
import { SectionPage, SettingRows, SettingsCard } from "../SettingsPrimitives";

export function LibrarySection() {
  const { setStatus, setError } = useSettings();
  const [categories, setCategories] = useState<Category[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [name, setName] = useState("");
  const [creating, setCreating] = useState(false);
  const [confirmRemoval, setConfirmRemoval] = useState<Category>();
  const [removing, setRemoving] = useState(false);

  const loadCategories = useCallback(async () => {
    try {
      setCategories(await api.categories());
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to load categories");
    } finally {
      setLoaded(true);
    }
  }, [setError]);
  useEffect(() => {
    void loadCategories();
  }, [loadCategories]);

  const addCategory = async () => {
    if (!name.trim()) return;
    setCreating(true);
    setError("");
    try {
      const next = await api.createCategory(name.trim());
      setCategories((items) => [...items, next]);
      setName("");
      setStatus(`Category ${next.name} added.`);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to create category");
    } finally {
      setCreating(false);
    }
  };
  // Removing a category unlinks it from titles but deletes no titles, so a
  // single confirmation step is enough.
  const removeCategory = async (category: Category) => {
    setRemoving(true);
    setError("");
    try {
      await api.deleteCategory(category.id);
      setConfirmRemoval(undefined);
      setStatus(`Category ${category.name} removed.`);
      await loadCategories();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to remove category");
    } finally {
      setRemoving(false);
    }
  };

  return (
    <SectionPage section="library">
      <SettingsCard title="Updates" description="Keep the library current automatically.">
        <SettingRows section="library" />
      </SettingsCard>
      <SettingsCard title="Categories" description="Group titles in the library view.">
        <div className="flex gap-2">
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="New category"
            aria-label="New category"
            className="min-w-0 flex-1 rounded-lg border border-zinc-800 bg-zinc-950 px-3 py-2 text-base sm:text-sm"
          />
          <button
            onClick={() => void addCategory()}
            disabled={creating || !name.trim()}
            className="rounded-lg bg-amber-400 px-3 py-2 text-sm font-semibold text-zinc-950 disabled:opacity-40"
          >
            {creating ? "Adding…" : "Add"}
          </button>
        </div>
        <div className="mt-3 flex flex-wrap gap-2">
          {categories.map((item) => (
            <span
              key={item.id}
              className="inline-flex items-center gap-1 rounded-full border border-zinc-700 py-1 pl-3 pr-1.5 text-xs"
            >
              {item.name}
              <button
                aria-label={`Delete category ${item.name}`}
                onClick={() => setConfirmRemoval(item)}
                className="rounded-full p-0.5 text-zinc-500 hover:text-red-300"
              >
                <X size={12} />
              </button>
            </span>
          ))}
          {loaded && categories.length === 0 && (
            <span className="text-xs text-zinc-500">No categories yet.</span>
          )}
        </div>
        {confirmRemoval && (
          <Modal
            title="Remove category"
            onClose={() => {
              if (!removing) setConfirmRemoval(undefined);
            }}
          >
            <p className="text-sm text-zinc-400">
              Remove the category {confirmRemoval.name}? Titles keep their other categories and stay
              in the library.
            </p>
            <div className="mt-5 flex justify-end gap-2">
              <button
                onClick={() => setConfirmRemoval(undefined)}
                disabled={removing}
                className="rounded-lg border border-zinc-700 px-3 py-2 text-sm disabled:opacity-50"
              >
                Cancel
              </button>
              <button
                onClick={() => void removeCategory(confirmRemoval)}
                disabled={removing}
                className="rounded-lg bg-red-500 px-3 py-2 text-sm font-semibold text-white disabled:opacity-50"
              >
                {removing ? "Removing…" : "Remove"}
              </button>
            </div>
          </Modal>
        )}
      </SettingsCard>
    </SectionPage>
  );
}
