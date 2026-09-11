import { useEffect, useState } from "react";
import { FileUp, LoaderCircle } from "lucide-react";
import { api } from "../../../api";
import type { Source, TachibackupReport, TachibackupSummary } from "../../../types";
import { useSettings } from "../SettingsLayout";
import { SettingsCard } from "../SettingsPrimitives";

// TachibackupImport restores a backup exported by the Android app. The file is
// validated first so sources that cannot be matched are shown before anything
// is written.
export function TachibackupImport() {
  const { reload, setStatus, setError } = useSettings();
  const [sources, setSources] = useState<Source[]>([]);
  const [file, setFile] = useState<File | null>(null);
  const [report, setReport] = useState<TachibackupReport | null>(null);
  const [mapping, setMapping] = useState<Record<number, string>>({});
  const [skipUnmatched, setSkipUnmatched] = useState(false);
  const [skipOutOfLibrary, setSkipOutOfLibrary] = useState(false);
  // The staging token returned by validation, so the import does not send the
  // file a second time.
  const [uploadId, setUploadId] = useState<string | null>(null);
  const [validating, setValidating] = useState(false);
  const [importing, setImporting] = useState(false);
  const [summary, setSummary] = useState<TachibackupSummary | null>(null);

  useEffect(() => {
    let active = true;
    api
      .sources()
      .then((list) => active && setSources(list))
      .catch(() => {
        // The mapping control needs the source list; a failure leaves it empty.
      });
    return () => {
      active = false;
    };
  }, []);

  const validate = async (selected: File) => {
    setValidating(true);
    setError("");
    setReport(null);
    setSummary(null);
    setFile(selected);
    setUploadId(null);
    try {
      const result = await api.validateTachibackup(selected);
      setReport(result);
      setUploadId(result.uploadId ?? null);
      const initial: Record<number, string> = {};
      for (const source of result.sources) {
        initial[source.backupSourceId] = source.matchedSourceId ?? "";
      }
      setMapping(initial);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to read the backup");
      setFile(null);
    } finally {
      setValidating(false);
    }
  };

  const runImport = async () => {
    if (!uploadId && !file) return;
    setImporting(true);
    setError("");
    try {
      const result = await api.importTachibackup(uploadId ?? file!, {
        sourceMap: mapping,
        skipUnmatched,
        skipOutOfLibrary,
      });
      setSummary(result);
      setReport(null);
      setFile(null);
      setUploadId(null);
      setStatus("Backup imported.");
      await reload();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Unable to import the backup");
    } finally {
      setImporting(false);
    }
  };

  return (
    <SettingsCard
      title="Import from another app"
      description="Restore a .tachibk backup exported by the Android app: library, categories, read state, history, and tracker links."
    >
      <label className="inline-flex cursor-pointer items-center gap-2 rounded-lg border border-zinc-700 px-3 py-2 text-sm">
        {validating ? <LoaderCircle size={15} className="animate-spin" /> : <FileUp size={15} />}
        {validating ? "Reading backup…" : "Choose .tachibk file"}
        <input
          type="file"
          accept=".tachibk,application/octet-stream"
          hidden
          disabled={validating || importing}
          onChange={(event) => {
            const selected = event.target.files?.[0];
            event.target.value = "";
            if (selected) void validate(selected);
          }}
        />
      </label>

      {report && (
        <div className="mt-4 space-y-4 text-sm">
          <div className="flex flex-wrap gap-x-6 gap-y-1 text-xs text-zinc-400">
            <span>{report.counts.manga} titles</span>
            <span>{report.counts.chapters} chapters</span>
            <span>{report.counts.categories} categories</span>
            <span>{report.counts.history} history entries</span>
            <span>{report.counts.trackings} tracker links</span>
            {report.unsupportedTrackings > 0 && (
              <span className="text-amber-300">
                {report.unsupportedTrackings} tracker links without a MakiDoku tracker will be
                skipped
              </span>
            )}
          </div>

          {report.sources.length > 0 && (
            <div className="space-y-2">
              <p className="text-xs uppercase tracking-wide text-zinc-500">Sources</p>
              {report.sources.map((source) => {
                const unmatched = !mapping[source.backupSourceId];
                return (
                  <div
                    key={source.backupSourceId}
                    className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-zinc-800 px-3 py-2"
                  >
                    <span className="min-w-0">
                      <b className="block truncate">
                        {source.name || `Source ${source.backupSourceId}`}
                      </b>
                      <small className="text-zinc-500">{source.mangaCount} titles</small>
                      {source.suggestedName && (
                        <small className="ml-2 text-amber-300">
                          looks like {source.suggestedName}
                          {source.detectedSite ? ` (${source.detectedSite})` : ""}
                        </small>
                      )}
                    </span>
                    <span className="flex items-center gap-2">
                      {!unmatched && source.match === "name" && (
                        <small className="text-emerald-400">matched by name</small>
                      )}
                      {!unmatched && source.match === "host" && (
                        <small className="text-emerald-400">matched by site</small>
                      )}
                      <select
                        value={mapping[source.backupSourceId] ?? ""}
                        onChange={(event) =>
                          setMapping((current) => ({
                            ...current,
                            [source.backupSourceId]: event.target.value,
                          }))
                        }
                        className="rounded-lg border border-zinc-700 bg-zinc-900 px-2 py-1 text-xs"
                      >
                        <option value="">No source (keep for later)</option>
                        {sources.map((candidate) => (
                          <option key={candidate.id} value={candidate.id}>
                            {candidate.name}
                          </option>
                        ))}
                      </select>
                    </span>
                  </div>
                );
              })}
            </div>
          )}

          {report.unmatchedTitles > 0 && (
            <label className="flex items-center gap-2 text-xs text-zinc-400">
              <input
                type="checkbox"
                checked={skipUnmatched}
                onChange={(event) => setSkipUnmatched(event.target.checked)}
              />
              Skip the {report.unmatchedTitles} titles whose source has no match
            </label>
          )}

          {report.outOfLibraryTitles > 0 && (
            <label className="flex items-center gap-2 text-xs text-zinc-400">
              <input
                type="checkbox"
                checked={skipOutOfLibrary}
                onChange={(event) => setSkipOutOfLibrary(event.target.checked)}
              />
              Skip the {report.outOfLibraryTitles} titles outside the library
            </label>
          )}

          <button
            onClick={() => void runImport()}
            disabled={importing}
            className="inline-flex items-center gap-2 rounded-lg bg-amber-400 px-3 py-2 text-sm font-semibold text-zinc-950 disabled:opacity-40"
          >
            {importing && <LoaderCircle size={15} className="animate-spin" />}
            {importing ? "Importing…" : `Import ${report.counts.manga} titles`}
          </button>
        </div>
      )}

      {summary && (
        <p className="mt-4 text-sm text-zinc-400">
          Restored {summary.manga} titles, {summary.chapters} chapters and {summary.categories}{" "}
          categories. {summary.mergedManga} titles merged into existing entries,{" "}
          {summary.deferredManga} kept without a source, {summary.skippedManga} skipped.{" "}
          {summary.tracking} tracker links restored, {summary.skippedTracking} skipped.{" "}
          {summary.outOfLibrary} titles outside the library, {summary.feeds} feeds,{" "}
          {summary.savedSearches} saved searches and {summary.merges} merged sources restored.
        </p>
      )}
    </SettingsCard>
  );
}
