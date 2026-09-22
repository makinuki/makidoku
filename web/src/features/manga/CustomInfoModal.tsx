import { useState } from "react";
import { RotateCw } from "lucide-react";
import { api } from "../../api";
import type { Manga } from "../../types";
import { Modal } from "../../components/Modal";

// A custom-info field maps a form control onto one override. An empty form
// value means "no override", so the source value is shown as the placeholder.
type FieldKey = "title" | "artist" | "author" | "description" | "genres" | "status" | "coverUrl";

const fields: {
  key: FieldKey;
  label: string;
  custom: (manga: Manga) => string;
  source: (manga: Manga) => string;
  multiline?: boolean;
}[] = [
  { key: "title", label: "Title", custom: (m) => m.customTitle ?? "", source: (m) => m.title },
  {
    key: "artist",
    label: "Artist",
    custom: (m) => m.customArtist ?? "",
    source: (m) => m.artists ?? "",
  },
  {
    key: "author",
    label: "Author",
    custom: (m) => m.customAuthor ?? "",
    source: (m) => m.authors ?? "",
  },
  {
    key: "description",
    label: "Description",
    custom: (m) => m.customDescription ?? "",
    source: (m) => m.description ?? "",
    multiline: true,
  },
  {
    key: "genres",
    label: "Genres (comma separated)",
    custom: (m) => genreText(m.customGenres),
    source: (m) => genreText(m.genres),
  },
  {
    key: "status",
    label: "Status",
    custom: (m) => m.customStatus ?? "",
    source: (m) => m.status,
  },
  {
    key: "coverUrl",
    label: "Cover URL",
    custom: (m) => m.customCoverUrl ?? "",
    source: (m) => m.coverUrl,
  },
];

// genreText renders a stored genre value. The library stores genres as a JSON
// array, but a custom value can be edited here as a plain comma list.
function genreText(value?: string) {
  if (!value) return "";
  const trimmed = value.trim();
  if (trimmed.startsWith("[")) {
    try {
      const parsed: unknown = JSON.parse(trimmed);
      if (Array.isArray(parsed)) return parsed.join(", ");
    } catch {
      // A malformed value is shown verbatim rather than dropped.
    }
  }
  return trimmed;
}

// genreValue encodes the comma list as the JSON array the library stores.
function genreValue(text: string) {
  const parts = text
    .split(",")
    .map((part) => part.trim())
    .filter(Boolean);
  return parts.length ? JSON.stringify(parts) : "";
}

export function CustomInfoModal({
  manga,
  onClose,
  onSaved,
}: {
  manga: Manga;
  onClose: () => void;
  onSaved: () => Promise<void>;
}) {
  const initial = Object.fromEntries(
    fields.map((field) => [field.key, field.custom(manga)]),
  ) as Record<FieldKey, string>;
  const [values, setValues] = useState<Record<FieldKey, string>>(initial);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  const save = async () => {
    setSaving(true);
    setError("");
    try {
      // Only the fields that actually changed are sent; clearing a field sends
      // an empty string so the override is dropped.
      const update: Record<string, string> = {};
      for (const field of fields) {
        const next = values[field.key];
        if (next === initial[field.key]) continue;
        update[field.key] = field.key === "genres" ? genreValue(next) : next;
      }
      if (Object.keys(update).length === 0) {
        onClose();
        return;
      }
      await api.updateMangaCustom(manga.id, update);
      await onSaved();
      onClose();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to save the custom info");
    } finally {
      setSaving(false);
    }
  };

  return (
    <Modal title="Custom info" onClose={onClose}>
      <p className="mb-4 text-xs text-zinc-500">
        Your text replaces what the source shows. Leave a field empty to use the source's value.
      </p>
      <div className="space-y-3">
        {fields.map((field) => (
          <label key={field.key} className="block">
            <span className="mb-1 block text-xs uppercase tracking-wide text-zinc-500">
              {field.label}
            </span>
            <span className="flex items-start gap-2">
              {field.multiline ? (
                <textarea
                  value={values[field.key]}
                  placeholder={field.source(manga)}
                  onChange={(event) =>
                    setValues((current) => ({ ...current, [field.key]: event.target.value }))
                  }
                  rows={3}
                  className="min-w-0 flex-1 rounded-lg border border-zinc-800 bg-zinc-950 px-3 py-2 text-sm"
                />
              ) : (
                <input
                  value={values[field.key]}
                  placeholder={field.source(manga)}
                  onChange={(event) =>
                    setValues((current) => ({ ...current, [field.key]: event.target.value }))
                  }
                  className="min-w-0 flex-1 rounded-lg border border-zinc-800 bg-zinc-950 px-3 py-2 text-sm"
                />
              )}
              <button
                type="button"
                aria-label={`Reset ${field.label} to the source value`}
                disabled={values[field.key] === ""}
                onClick={() => setValues((current) => ({ ...current, [field.key]: "" }))}
                className="rounded-lg border border-zinc-700 p-2 text-zinc-400 hover:text-white disabled:opacity-30"
              >
                <RotateCw size={15} />
              </button>
            </span>
          </label>
        ))}
      </div>
      {error && (
        <p role="alert" className="mt-3 text-xs text-red-300">
          {error}
        </p>
      )}
      <div className="mt-5 flex justify-end gap-2">
        <button onClick={onClose} className="rounded-lg border border-zinc-700 px-3 py-2 text-sm">
          Cancel
        </button>
        <button
          onClick={() => void save()}
          disabled={saving}
          className="rounded-lg bg-amber-400 px-3 py-2 text-sm font-semibold text-zinc-950 disabled:opacity-40"
        >
          {saving ? "Saving…" : "Save"}
        </button>
      </div>
    </Modal>
  );
}
