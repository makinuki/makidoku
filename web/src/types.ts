export type Category = { id: number; name: string; sortOrder: number };
export type BulkResult = { updated: number; failed: { id: string; error: string }[] };
export type Manga = {
  id: string;
  sourceId: string;
  title: string;
  altTitles?: string;
  description?: string;
  authors?: string;
  artists?: string;
  genres?: string;
  // Secondary descriptors (themes, formats, demographics) when the source
  // distinguishes them from genres. Stored as a JSON-encoded array.
  tags?: string;
  status: string;
  coverUrl: string;
  inLibrary: boolean;
  downloadFormat: string;
  downloadNewChapters?: boolean;
  readerMode?: "single" | "double" | "webtoon" | null;
  readerDirection?: "ltr" | "rtl" | null;
  readerFit?: "width" | "height" | "original" | null;
  // Per-title chapter list presentation. An absent value means unset and the
  // client falls back to its defaults.
  chapterSort?: "number-desc" | "number-asc" | "source";
  chapterFilter?: "all" | "unread" | "downloaded" | "bookmarked";
  chapterLanguage?: string;
  createdAt: number;
  updatedAt: number;
  detailsFetchedAt?: number;
  // User overrides restored from a backup or set in the custom-info dialog.
  // A set value wins over the source value at render time.
  customTitle?: string;
  customArtist?: string;
  customAuthor?: string;
  customDescription?: string;
  customGenres?: string;
  customStatus?: string;
  customCoverUrl?: string;
  notes?: string;
  displayTitle?: string;
  displayDescription?: string;
  displayAuthors?: string;
  displayArtists?: string;
  displayGenres?: string;
  displayStatus?: string;
  displayCoverUrl?: string;
};
export type Chapter = {
  id: string;
  mangaId: string;
  chapterNumber?: number;
  volume?: number;
  title?: string;
  language?: string;
  uploadedAt?: number;
  scanlator?: string;
  locked?: boolean;
  downloaded: boolean;
  downloadPath?: string;
  read?: boolean;
  bookmark?: boolean;
  downloadStatus?: string;
};
export type Progress = {
  mangaId: string;
  lastReadChapterId: string;
  lastReadPage: number;
  totalPages: number;
  isCompleted: boolean;
  lastReadAt: number;
};
export type HistoryEvent = {
  id: string;
  manga: Manga;
  chapter?: Chapter;
  page?: number;
  occurredAt: number;
};
export type Binding = {
  id: number;
  mangaId: string;
  trackerType: string;
  remoteId: string;
  remoteTitle: string;
  remoteScore?: number;
  remoteStatus?: string;
  lastSyncedChapter: number;
  totalRemoteChapters?: number;
  startedAt?: number;
  finishedAt?: number;
};
export type TrackerAuthType = "oauth" | "password" | "token";

export type TrackerInfo = {
  name: string;
  capabilities: {
    oauth: boolean;
    token: boolean;
  };
  credential: boolean;
  authType: TrackerAuthType;
  configured: boolean;
  configHint?: string;
  connectedAs?: string;
};
export type TrackerSearchResult = {
  remoteId: string;
  title: string;
  score?: number;
  chapters?: number;
  status?: string;
  coverUrl?: string;
};
export type LibraryManga = Manga & {
  categories: Category[];
  progress?: Progress;
  unreadChapters: number;
  downloadedChapters: number;
  totalChapters: number;
  bookmarkedChapters: number;
  languages: string[];
  sourceName?: string;
};
export type Aggregate = {
  manga: Manga;
  categories: Category[];
  chapters: Chapter[];
  progress?: Progress;
  trackers: Binding[];
  readingSeconds?: number;
  sourceName?: string;
  sourceUrl?: string;
  // Present when a details refresh failed but the stored record was served.
  refreshError?: string;
};
export type Recommendation = {
  remoteId: string;
  title: string;
  score?: number;
  chapters?: number;
  status?: string;
  coverUrl?: string;
};
export type Source = {
  id: string;
  name: string;
  version: string;
  abiVersion: number;
  lang: string;
  baseUrl: string;
  iconUrl: string;
  nsfw: boolean;
  installedAt: number;
  loaded: boolean;
  hasClearance: boolean;
  hasSettings?: boolean;
  allowedHosts?: string[];
  pinned?: boolean;
  lastUsedAt?: number;
};
export type CatalogEntry = Source & {
  installed: boolean;
  installedVersion?: string;
  compatible: boolean;
  incompatibility?: string;
  wasmUrl?: string;
  sha256?: string;
  updateAvailable?: boolean;
};
export type Health = { ok: boolean; database?: string };
export type SearchResult = {
  id: string;
  title: string;
  coverUrl: string;
  latestChapter?: string;
  url?: string;
};
export type Page = {
  id: string;
  chapterId: string;
  index: number;
  isScrambled: boolean;
};
export type PageResult = { page: number; hasNextPage: boolean; items: SearchResult[] };
export type FilterOption = { label: string; value: string };
export type FilterSchema =
  | { id: string; title: string; type: "select"; options: FilterOption[]; default: string }
  | {
      id: string;
      title: string;
      type: "tri_state";
      options: FilterOption[];
      default?: Record<string, "+" | "-">;
    }
  | { id: string; title: string; type: "checkbox"; default: boolean }
  | { id: string; title: string; type: "text"; placeholder?: string; default?: string };
export type SourceSettingOption = { label: string; value: string };
// One setting a source declares through get_settings, together with its
// stored state. A sensitive setting never carries its value, only hasValue.
export type SourceSetting = {
  id: string;
  title: string;
  description?: string;
  type: "checkbox" | "select" | "text";
  options?: SourceSettingOption[];
  placeholder?: string;
  default?: boolean | string;
  sensitive?: boolean;
  value?: boolean | string;
  hasValue: boolean;
};
export type CoverVariant = { url: string; width?: number; height?: number };
export type Details = {
  id: string;
  title: string;
  altTitles?: string[];
  description?: string;
  authors?: string[];
  artists?: string[];
  genres?: string[];
  status: string;
  coverUrl?: string;
  covers?: CoverVariant[];
  chapters: Array<{
    id: string;
    number?: number;
    volume?: number;
    language?: string;
    title?: string;
    uploadedAt?: number;
    scanlator?: string;
  }>;
};
export type QueueItem = {
  id: number;
  chapterId: string;
  status: string;
  progress: number;
  totalPages: number;
  downloadedPages: number;
  errorMessage?: string;
  mangaId: string;
  mangaTitle: string;
  sourceId: string;
  sourceName: string;
  chapterNumber?: number;
  chapterTitle?: string;
  uploadedAt?: number;
  // Downloader order of the row; the worker claims the lowest position first.
  position: number;
};
export type DownloadSnapshot = {
  items: QueueItem[];
  stats: { downloadedPages: number; retriedRequests: number; throttledRequests: number };
  paused: boolean;
};
export type DownloadEvent = {
  type: string;
  // Item-less events announce a downloader-level change: "state" carries the
  // paused flag and the counters, "reordered" announces an order change.
  item?: QueueItem;
  stats: DownloadSnapshot["stats"];
  paused: boolean;
};
export type TrackerStatus = {
  trackerType?: string;
  remoteId: string;
  title: string;
  status?: string;
  score?: number;
  progress: number;
  totalChapters?: number;
  startedAt?: number;
  finishedAt?: number;
};
export type TrackerSyncJob = {
  id: number;
  mangaId: string;
  bindingId: number;
  chapterNumber: number;
  status: string;
  attempts: number;
  errorMessage?: string;
};
export type UpdateLog = {
  id: string;
  manga: Manga;
  chapter: Chapter;
  seenAt: number;
  acknowledged: boolean;
};
export type LibraryUpdateState = { lastRunAt?: number; lastStatus: string };
export type ReadingDay = { date: string; seconds: number };
export type ReadingStats = {
  readingSeconds: number;
  titleCount: number;
  chapterCount: number;
  daily: ReadingDay[];
  overview: { libraryMangaCount: number; completedMangaCount: number; totalReadDuration: number };
  titles: { updateEnabledCount: number; startedMangaCount: number };
  chapters: { totalChapterCount: number; readChapterCount: number; downloadCount: number };
  trackers: { trackedTitleCount: number; meanScore: number; trackerCount: number };
  topTitles: { mangaId: string; title: string; seconds: number; chaptersRead: number }[];
};
export type MigrationCandidate = { source: Source; result: SearchResult };
export type MigrationCandidates = {
  candidates: MigrationCandidate[];
  failedSources: number;
  searched: number;
};
export type MigrationResponse = {
  manga: Aggregate;
  source: string;
  chapterMap: Record<string, string>;
};
export type MigrationSource = { source: Source; count: number; imported?: boolean };

export type MangaMerge = {
  id: string;
  mangaId: string;
  sourceId: string;
  sourceMangaId: string;
  url?: string;
  isInfoManga: boolean;
  getChapterUpdates: boolean;
  chapterSortMode: number;
  chapterPriority: number;
  downloadChapters: boolean;
  mergeOrder: number;
};

export type MangaMetadata = {
  metadata?: {
    mangaId: string;
    uploader?: string;
    extra: string;
    indexedExtra?: string;
    extraVersion: number;
  };
  titles: { id: string; mangaId: string; title: string; titleType: number }[];
  tags: { id: string; mangaId: string; namespace?: string; name: string; tagType: number }[];
};

export type Feed = { id: string; sourceId: string; global: boolean; feedOrder: number };

export type SavedSearch = {
  id: string;
  sourceId: string;
  feedId?: string;
  name: string;
  query: string;
  filters: string;
  searchOrder: number;
};

export type TachibackupCounts = {
  manga: number;
  chapters: number;
  categories: number;
  history: number;
  trackings: number;
};

export type TachibackupSourceReport = {
  backupSourceId: number;
  name: string;
  mangaCount: number;
  matchedSourceId?: string;
  matchedSourceName?: string;
  match?: string;
  deferred: boolean;
  detectedSite?: string;
  suggestedName?: string;
  sampleTitles?: string[];
  sampleURL?: string;
};

export type TachibackupTrackerReport = {
  syncId: number;
  name?: string;
  trackerType?: string;
  supported: boolean;
  mangaCount: number;
};

export type TachibackupReport = {
  counts: TachibackupCounts;
  sources: TachibackupSourceReport[];
  trackers: TachibackupTrackerReport[];
  unmatchedTitles: number;
  unsupportedTrackings: number;
  outOfLibraryTitles: number;
  preferences: number;
  sourcePreferences: number;
  extensionStores: number;
  uploadId: string;
};

export type TachibackupOptions = {
  sourceMap?: Record<number, string>;
  skipUnmatched?: boolean;
  skipOutOfLibrary?: boolean;
};

export type TachibackupProgress = {
  phase: string;
  processed: number;
  total: number;
};

export type TachibackupSummary = {
  categories: number;
  manga: number;
  mergedManga: number;
  deferredManga: number;
  skippedManga: number;
  outOfLibrary: number;
  chapters: number;
  readChapters: number;
  history: number;
  readingSessions: number;
  tracking: number;
  skippedTracking: number;
  feeds: number;
  savedSearches: number;
  merges: number;
  metadata: number;
};
