import type {
  Aggregate,
  Category,
  DownloadSnapshot,
  LibraryManga,
  Page,
  PageResult,
  Progress,
  Source,
  Binding,
  CatalogEntry,
  MigrationCandidate,
  MigrationResponse,
  TrackerInfo,
  TrackerSearchResult,
  Health,
  TrackerStatus,
  TrackerSyncJob,
  FilterSchema,
} from "./types";

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    ...init,
    headers: { "Content-Type": "application/json", ...init?.headers },
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
  categories: () => request<Category[]>("/api/categories"),
  createCategory: (name: string) =>
    request<Category>("/api/categories", { method: "POST", body: JSON.stringify({ name }) }),
  deleteCategory: (id: number) => request<void>(`/api/categories/${id}`, { method: "DELETE" }),
  setCategory: (mangaId: string, category: number, enabled: boolean) =>
    request(`/api/manga/${idPath(mangaId)}/categories/${category}`, {
      method: enabled ? "POST" : "DELETE",
    }),
  history: () =>
    request<Array<{ manga: LibraryManga; chapter: import("./types").Chapter; progress: Progress }>>(
      "/api/history",
    ),
  sources: () => request<Source[]>("/api/sources"),
  catalog: () => request<CatalogEntry[]>("/api/sources/catalog"),
  installSource: (id: string) =>
    request<Source>("/api/sources/install", { method: "POST", body: JSON.stringify({ id }) }),
  uninstallSource: (id: string) =>
    request<void>(`/api/sources/${encodeURIComponent(id)}/`, { method: "DELETE" }),
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
  progress: (mangaId: string, chapterId: string, page: number, total: number, complete = false) =>
    request<Progress>(`/api/progress${complete ? "/complete" : ""}`, {
      method: "POST",
      body: JSON.stringify({
        mangaId,
        lastReadChapterId: chapterId,
        lastReadPage: page,
        totalPages: total,
        isCompleted: complete,
        lastReadAt: 0,
      }),
    }),
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
  trackerStatuses: (mangaId: string) =>
    request<TrackerStatus[]>(`/api/manga/${idPath(mangaId)}/trackers/status`),
  syncJobs: () => request<TrackerSyncJob[]>("/api/tracker-sync"),
  migrationCandidates: (mangaId: string, query?: string) =>
    request<MigrationCandidate[]>(
      `/api/manga/${idPath(mangaId)}/migration/candidates${query ? `?q=${encodeURIComponent(query)}` : ""}`,
    ),
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
  readerImage: (page: Page) => `/api/pages/${idPath(page.id)}/image`,
};

export type Api = typeof api;
