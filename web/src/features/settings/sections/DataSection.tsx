import { useCallback, useEffect, useState } from "react";
import { Download, FolderOpen, LoaderCircle } from "lucide-react";
import { api } from "../../../api";
import { Modal } from "../../../components/Modal";
import { useSettings } from "../SettingsLayout";
import { SectionPage, SettingRows, SettingsCard } from "../SettingsPrimitives";

export function DataSection() {
  const { reload, setStatus, setError } = useSettings();
  const [exporting, setExporting] = useState(false);
  const [confirmImport, setConfirmImport] = useState(false);
  const [importing, setImporting] = useState(false);
  const [readingSeconds, setReadingSeconds] = useState(0);
  const [titleCount, setTitleCount] = useState(0);
  const [chapterCount, setChapterCount] = useState(0);

  const loadStats = useCallback(async () => {
    try {
      const stats = await api.stats();
      setReadingSeconds(stats.readingSeconds || 0);
      setTitleCount(stats.titleCount || 0);
      setChapterCount(stats.chapterCount || 0);
    } catch {
      // Keep the last values until the next successful read.
    }
  }, []);
  useEffect(() => {
    void loadStats();
  }, [loadStats]);

  // The export is fetched to a blob instead of a plain link so a failed
  // request surfaces as a message rather than navigating away from the app.
  const exportJson = async () => {
    setExporting(true);
    setError("");
    try {
      const response = await fetch("/api/export");
      if (!response.ok) throw new Error(`Export failed (${response.status})`);
      const blob = await response.blob();
      const url = URL.createObjectURL(blob);
      const anchor = document.createElement("a");
      anchor.href = url;
      anchor.download = `makidoku-backup-${new Date().toISOString().slice(0, 10)}.json`;
      anchor.click();
      URL.revokeObjectURL(url);
      setStatus("Backup exported.");
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to export backup");
    } finally {
      setExporting(false);
    }
  };
  // Importing overwrites library, progress and categories, so the file is
  // only picked after an explicit confirmation. A success re-reads the values
  // the restore may have changed.
  const runImport = async (file: File) => {
    setImporting(true);
    setError("");
    try {
      await api.importBackup(file);
      setStatus("Backup imported.");
      await Promise.all([loadStats(), reload()]);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to import backup");
    } finally {
      setImporting(false);
    }
  };

  return (
    <SectionPage section="data">
      <SettingsCard title="Automatic backup" description="Scheduled snapshots of the library.">
        <SettingRows section="data" />
      </SettingsCard>
      <SettingsCard title="Backup" description="Take a full copy or restore one.">
        <div className="flex flex-wrap gap-2">
          <button
            onClick={() => void exportJson()}
            disabled={exporting}
            className="inline-flex items-center gap-2 rounded-lg bg-amber-400 px-3 py-2 text-sm font-semibold text-zinc-950 disabled:opacity-40"
          >
            {exporting ? (
              "Preparing…"
            ) : (
              <>
                <Download size={15} /> Export JSON
              </>
            )}
          </button>
          <button
            onClick={() => setConfirmImport(true)}
            className="inline-flex items-center gap-2 rounded-lg border border-zinc-700 px-3 py-2 text-sm"
          >
            <FolderOpen size={15} /> Import JSON
          </button>
        </div>
        {confirmImport && (
          <Modal
            title="Import backup"
            onClose={() => {
              if (!importing) setConfirmImport(false);
            }}
          >
            <p className="text-sm text-zinc-400">
              Importing replaces your library, categories, progress and tracker links with the
              contents of the selected file. This cannot be undone.
            </p>
            <div className="mt-5 flex justify-end gap-2">
              <button
                onClick={() => setConfirmImport(false)}
                disabled={importing}
                className="rounded-lg border border-zinc-700 px-3 py-2 text-sm disabled:opacity-50"
              >
                Cancel
              </button>
              <label
                className={`inline-flex cursor-pointer items-center gap-2 rounded-lg bg-amber-400 px-3 py-2 text-sm font-semibold text-zinc-950 ${
                  importing ? "pointer-events-none opacity-60" : ""
                }`}
              >
                {importing && <LoaderCircle size={15} className="animate-spin" />}
                {importing ? "Importing…" : "Choose file"}
                <input
                  type="file"
                  accept="application/json"
                  hidden
                  disabled={importing}
                  onChange={(event) => {
                    const file = event.target.files?.[0];
                    event.target.value = "";
                    if (!file) return;
                    setConfirmImport(false);
                    void runImport(file);
                  }}
                />
              </label>
            </div>
          </Modal>
        )}
      </SettingsCard>
      <SettingsCard title="Reading statistics" description="Time recorded by the desktop reader.">
        <div className="flex items-baseline gap-3">
          <strong className="text-3xl font-semibold text-amber-300">
            {formatReadingTime(readingSeconds)}
          </strong>
          <span className="text-xs uppercase tracking-wide text-zinc-500">total reading time</span>
        </div>
        <div className="mt-4 flex gap-6 text-xs text-zinc-500">
          <span>{titleCount} titles</span>
          <span>{chapterCount} chapters</span>
        </div>
      </SettingsCard>
    </SectionPage>
  );
}

function formatReadingTime(seconds: number) {
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes} min`;
  const hours = Math.floor(minutes / 60);
  const remainder = minutes % 60;
  return remainder ? `${hours}h ${remainder}m` : `${hours}h`;
}
