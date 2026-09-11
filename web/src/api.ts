import type {
  Aggregate,
  BulkResult,
  Category,
  DownloadSnapshot,
  LibraryManga,
  Page,
  PageResult,
  Progress,
  Source,
  Binding,
  CatalogEntry,
  MigrationCandidates,
  MigrationResponse,
  TrackerInfo,
  TrackerSearchResult,
  Health,
  TrackerStatus,
  TrackerSyncJob,
  FilterSchema,
  HistoryEvent,
  UpdateLog,
  LibraryUpdateState,
  Manga,
  MigrationSource,
  Recommendation,
  ReadingStats,
  TachibackupOptions,
  TachibackupProgress,
  TachibackupReport,
  TachibackupSummary,
} from "./types";

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const headers = new Headers();
  // FormData carries its own multipart content type with a generated boundary.
  if (!(init?.body instanceof FormData)) headers.set("Content-Type", "application/json");
  // Caller headers still win, as they did before the default was conditional.
  new Headers(init?.headers).forEach((value, key) => headers.set(key, value));
  const response = await fetch(path, {
    ...init,
    headers,
  });
  if (!response.ok) {
    const payload = await response.json().catch(() => ({}));
    throw new Error(payload?.error?.message || `Request failed (${response.status})`);
  }
  if (response.status === 204) return undefined as T;
  return response.json() as Promise<T>;
}

function idPath(id: string) {
  return encodeURIComponent(id);
}

export const api = {
  health: () => request<Health>("/api/health"),
  library: (query = "", category?: number) =>
    request<LibraryManga[]>(
      `/api/library?q=${encodeURIComponent(query)}${category ? `&category=${category}` : ""}`,
    ),
  manga: (mangaId: string) => request<Aggregate>(`/api/manga/${idPath(mangaId)}`),
  refreshManga: (mangaId: string) =>
    request<Aggregate>(`/api/manga/${idPath(mangaId)}/refresh`, { method: "POST" }),
  saveManga: (mangaId: string) =>
    request<Aggregate>(`/api/manga/${idPath(mangaId)}/library`, { method: "POST" }),
  setLibrary: (mangaId: string, enabled: boolean) =>
    request(`/api/manga/${idPath(mangaId)}/library`, { method: enabled ? "POST" : "DELETE" }),
  setAutoDownload: (mangaId: string, enabled: boolean) =>
    request<Manga>(`/api/manga/${idPath(mangaId)}/downloads`, {
      method: "PATCH",
      body: JSON.stringify({ enabled }),
    }),
  setMangaReaderOverrides: (
    mangaId: string,
    overrides: { mode: string | null; direction: string | null; fit: string | null },
  ) =>
    request<Manga>(`/api/manga/${idPath(mangaId)}/reader`, {
      method: "PATCH",
      body: JSON.stringify(overrides),
    }),
  categories: () => request<Category[]>("/api/categories"),
  createCategory: (name: string) =>
    request<Category>("/api/categories", { method: "POST", body: JSON.stringify({ name }) }),
  deleteCategory: (id: number) => request<void>(`/api/categories/${id}`, { method: "DELETE" }),
  setCategory: (mangaId: string, category: number, enabled: boolean) =>
    request(`/api/manga/${idPath(mangaId)}/categories/${category}`, {
      method: enabled ? "POST" : "DELETE",
    }),
  bulkSetRead: (ids: string[], read: boolean) =>
    request<BulkResult>("/api/library/bulk/read", {
      method: "POST",
      body: JSON.stringify({ ids, read }),
    }),
  bulkSetLibrary: (ids: string[], inLibrary: boolean) =>
    request<BulkResult>("/api/library/bulk/library", {
      method: "POST",
      body: JSON.stringify({ ids, inLibrary }),
    }),
  bulkSetCategory: (ids: string[], categoryId: number, enabled: boolean) =>
    request<BulkResult>("/api/library/bulk/category", {
      method: "POST",
      body: JSON.stringify({ ids, categoryId, enabled }),
    }),
  bulkDownload: (ids: string[], chapters: "unread" | "all") =>
    request<BulkResult>("/api/library/bulk/download", {
      method: "POST",
      body: JSON.stringify({ ids, chapters }),
    }),
  bulkRemove: (ids: string[]) =>
    request<BulkResult>("/api/library/bulk", {
      method: "DELETE",
      body: JSON.stringify({ ids }),
    }),
  history: () => request<HistoryEvent[]>("/api/history"),
  deleteHistoryEvent: (id: string) =>
    request<void>(`/api/history/${encodeURIComponent(id)}`, { method: "DELETE" }),
  setChapterRead: (chapterId: string, read: boolean) =>
    request(`/api/chapters/${encodeURIComponent(chapterId)}/read`, {
      method: "POST",
      body: JSON.stringify({ read }),
    }),
  setMangaChaptersRead: (mangaId: string, chapterIds: string[], read: boolean) =>
    request(`/api/manga/${encodeURIComponent(mangaId)}/chapters/read`, {
      method: "POST",
      body: JSON.stringify({ chapterIds, read }),
    }),
  updates: (all = false) => request<UpdateLog[]>(`/api/updates${all ? "?all=true" : ""}`),
  updateState: () => request<LibraryUpdateState | null>("/api/updates/state"),
  runUpdates: () => request<{ new: number }>("/api/updates/run", { method: "POST" }),
  acknowledgeUpdates: (ids: string[]) =>
    request<void>("/api/updates/ack", { method: "POST", body: JSON.stringify({ ids }) }),
  settings: () => request<RuntimeSetting[]>("/api/settings"),
  incognito: () => request<{ enabled: boolean }>("/api/incognito"),
  setIncognito: (enabled: boolean) =>
    request<{ enabled: boolean }>("/api/incognito", {
      method: "POST",
      body: JSON.stringify({ enabled }),
    }),
  updateSetting: (key: string, value: unknown) =>
    request(`/api/settings/${encodeURIComponent(key)}`, {
      method: "PUT",
      body: JSON.stringify({ value }),
    }),
  sources: () => request<Source[]>("/api/sources"),
  catalog: () => request<CatalogEntry[]>("/api/sources/catalog"),
  installSource: (id: string) =>
    request<Source>("/api/sources/install", { method: "POST", body: JSON.stringify({ id }) }),
  uninstallSource: (id: string) =>
    request<void>(`/api/sources/${encodeURIComponent(id)}/`, { method: "DELETE" }),
  updateSource: (id: string) =>
    request<Source>(`/api/sources/${encodeURIComponent(id)}/update`, { method: "POST" }),
  updateAllSources: () => request<Source[]>("/api/sources/update-all", { method: "POST" }),
  pinSource: (id: string, pinned: boolean) =>
    request<Source>(`/api/sources/${encodeURIComponent(id)}/`, {
      method: "PATCH",
      body: JSON.stringify({ pinned }),
    }),
  sourceIcon: (id: string) => `/api/sources/${encodeURIComponent(id)}/icon`,
  sourceFilters: (id: string) =>
    request<FilterSchema[]>(`/api/sources/${encodeURIComponent(id)}/filters`),
  submitClearance: (id: string, cookie: string, userAgent: string) =>
    request<Source>(`/api/sources/${encodeURIComponent(id)}/clearance`, {
      method: "POST",
      body: JSON.stringify({ cookie, userAgent }),
    }),
  search: (source: string, q: string, page = 1, filters?: Record<string, unknown>) =>
    request<PageResult>(
      `/api/sources/${encodeURIComponent(source)}/search?q=${encodeURIComponent(q)}&page=${page}${filters && Object.keys(filters).length ? `&filters=${encodeURIComponent(JSON.stringify(filters))}` : ""}`,
    ),
  pages: (chapterId: string) => request<Page[]>(`/api/chapters/${idPath(chapterId)}/pages`),
  enqueue: (mangaId: string, chapters: string[], range = "", format = "") =>
    request<DownloadSnapshot>("/api/download", {
      method: "POST",
      body: JSON.stringify({ mangaId, chapters, range, format }),
    }),
  downloads: () => request<DownloadSnapshot>("/api/download"),
  controlDownload: (id: number, action: "pause" | "resume" | "cancel" | "retry") =>
    request(`/api/download/${id}/${action}`, { method: "POST" }),
  clearFinishedDownloads: () =>
    request<{ removed: number }>("/api/download/clear", { method: "POST" }),
  progress: (
    mangaId: string,
    chapterId: string,
    page: number,
    total: number,
    complete = false,
    sessionSeconds = 0,
  ) =>
    request<Progress>(`/api/progress${complete ? "/complete" : ""}`, {
      method: "POST",
      body: JSON.stringify({
        mangaId,
        lastReadChapterId: chapterId,
        lastReadPage: page,
        totalPages: total,
        isCompleted: complete,
        lastReadAt: 0,
        sessionSeconds,
      }),
    }),
  stats: () => request<ReadingStats>("/api/stats"),
  suggestions: (mangaId: string) =>
    request<Recommendation[]>(`/api/manga/${idPath(mangaId)}/suggestions`),
  progressGet: (mangaId: string) => request<Progress | null>(`/api/progress/${idPath(mangaId)}`),
  trackerBindings: (mangaId: string) =>
    request<Binding[]>(`/api/manga/${idPath(mangaId)}/trackers/`),
  trackers: () => request<TrackerInfo[]>("/api/trackers"),
  trackerSearch: (type: string, query: string) =>
    request<TrackerSearchResult[]>(
      `/api/trackers/${encodeURIComponent(type)}/search?q=${encodeURIComponent(query)}`,
    ),
  saveTrackerToken: (type: string, accessToken: string, metadata?: Record<string, string>) =>
    request<void>(`/api/trackers/${encodeURIComponent(type)}/token`, {
      method: "POST",
      body: JSON.stringify({ accessToken, metadata }),
    }),
  trackerLogin: (type: string, username: string, password: string) =>
    request<void>(`/api/trackers/${encodeURIComponent(type)}/login`, {
      method: "POST",
      body: JSON.stringify({ username, password }),
    }),
  deleteTrackerCredentials: (type: string) =>
    request<void>(`/api/trackers/${encodeURIComponent(type)}/credentials`, { method: "DELETE" }),
  startTrackerAuth: (type: string) =>
    request<{ authorizationUrl: string; redirectUri: string }>(
      `/api/trackers/${encodeURIComponent(type)}/auth/start`,
    ),
  bindTracker: (mangaId: string, type: string, value: Partial<Binding>) =>
    request<Binding>(`/api/manga/${idPath(mangaId)}/trackers/${encodeURIComponent(type)}/bind`, {
      method: "POST",
      body: JSON.stringify({
        remoteId: value.remoteId,
        remoteTitle: value.remoteTitle,
        remoteScore: value.remoteScore,
        remoteStatus: value.remoteStatus,
        totalRemoteChapters: value.totalRemoteChapters,
      }),
    }),
  unbindTracker: (mangaId: string, type: string) =>
    request(`/api/manga/${idPath(mangaId)}/trackers/${encodeURIComponent(type)}`, {
      method: "DELETE",
    }),
  updateTrackerBinding: (
    mangaId: string,
    type: string,
    update: { score?: number; startedAt?: number; finishedAt?: number },
  ) =>
    request<Binding>(`/api/manga/${idPath(mangaId)}/trackers/${encodeURIComponent(type)}`, {
      method: "PATCH",
      body: JSON.stringify(update),
    }),
  trackerStatuses: (mangaId: string) =>
    request<TrackerStatus[]>(`/api/manga/${idPath(mangaId)}/trackers/status`),
  syncJobs: () => request<TrackerSyncJob[]>("/api/tracker-sync"),
  migrationCandidates: (mangaId: string, query?: string) =>
    request<MigrationCandidates>(
      `/api/manga/${idPath(mangaId)}/migration/candidates${query ? `?q=${encodeURIComponent(query)}` : ""}`,
    ),
  migrationSources: () => request<MigrationSource[]>("/api/migration/sources"),
  migrationSourceManga: (sourceId: string) =>
    request<Manga[]>(`/api/migration/sources/${encodeURIComponent(sourceId)}/manga`),
  applyMigration: (mangaId: string, replacementSourceId: string, replacementMangaId: string) =>
    request<MigrationResponse>(`/api/manga/${idPath(mangaId)}/migration/apply`, {
      method: "POST",
      body: JSON.stringify({ sourceId: replacementSourceId, mangaId: replacementMangaId }),
    }),
  importBackup: (file: File) =>
    request<void>("/api/import", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: file,
    }),
  validateTachibackup: (file: File) => {
    const body = new FormData();
    body.append("file", file);
    return request<TachibackupReport>("/api/backup/tachibackup/validate", { method: "POST", body });
  },
  // input is either a File to upload or the uploadId returned by a prior
  // validation, which avoids sending the backup twice.
  importTachibackup: (input: File | string, options: TachibackupOptions) => {
    const body = new FormData();
    if (typeof input === "string") {
      body.append("uploadId", input);
    } else {
      body.append("file", input);
    }
    body.append("options", JSON.stringify(options));
    return request<TachibackupSummary>("/api/backup/tachibackup/import", { method: "POST", body });
  },
  // importTachibackupStream reads newline-delimited progress from the import
  // endpoint and resolves with the final summary.
  importTachibackupStream: async (
    input: File | string,
    options: TachibackupOptions,
    onProgress: (progress: TachibackupProgress) => void,
  ): Promise<TachibackupSummary> => {
    const body = new FormData();
    if (typeof input === "string") {
      body.append("uploadId", input);
    } else {
      body.append("file", input);
    }
    body.append("options", JSON.stringify(options));
    body.append("stream", "true");
    const response = await fetch("/api/backup/tachibackup/import", { method: "POST", body });
    if (!response.ok || !response.body) {
      const payload = await response.json().catch(() => ({}));
      throw new Error(payload?.error?.message || `Request failed (${response.status})`);
    }
    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let buffer = "";
    let summary: TachibackupSummary | undefined;
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      buffer += decoder.decode(value, { stream: true });
      for (let index = buffer.indexOf("\n"); index >= 0; index = buffer.indexOf("\n")) {
        const line = buffer.slice(0, index).trim();
        buffer = buffer.slice(index + 1);
        if (!line) continue;
        const parsed = JSON.parse(line) as {
          error?: string;
          summary?: TachibackupSummary;
          phase?: string;
          processed?: number;
          total?: number;
        };
        if (parsed.error) throw new Error(parsed.error);
        if (parsed.summary) {
          summary = parsed.summary;
        } else if (parsed.phase !== undefined) {
          onProgress({
            phase: parsed.phase,
            processed: parsed.processed ?? 0,
            total: parsed.total ?? 0,
          });
        }
      }
    }
    if (!summary) throw new Error("The import ended without a summary");
    return summary;
  },
  readerImage: (page: Page) => `/api/pages/${idPath(page.id)}/image`,
};

export type Api = typeof api;

export type RuntimeSetting = {
  key: string;
  value: string | number | boolean;
  default: string | number | boolean;
  type: string;
  description: string;
  // Hidden view state is stored by the daemon but owned by a screen, so the
  // settings page leaves it out.
  hidden?: boolean;
};
