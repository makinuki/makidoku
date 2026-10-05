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
  // languageFilter is the chapter language selection in effect for the source.
  languageFilter?: string[];
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
  // languages is the stored per-source chapter language selection; an empty
  // list follows the global default.
  languages?: string[];
  // availableLanguages lists the chapter language codes seen for the source.
  availableLanguages?: string[];
  allowedHosts?: string[];
  pinned?: boolean;
  lastUsedAt?: number;
  // challenge describes the anti-bot state of the source, or is absent when
  // the source has not met a check.
  challenge?: SourceChallenge;
  // clearance lists the stored clearance material, one entry per domain. Cookie
  // values are never sent to the client.
  clearance?: ClearanceSummary[];
};
// ClearanceSummary describes stored clearance for one domain without exposing
// any cookie value.
export type ClearanceSummary = {
  origin: string;
  hasClearance: boolean;
  cookies: string[];
  status: string;
  browserProfile: string;
  obtainedAt: number;
  expiresHint?: number;
  lastSuccessAt?: number;
  lastChallengeAt?: number;
  generation: number;
  // userAgent is the identity the solve ran under. It is shown because a
  // mismatch between the cookie and the agent is the most common cause of a
  // replay failing.
  userAgent?: string;
};

// SolverCapability reports whether this machine can present a challenge in a
// browser. Availability belongs to the machine rather than to a source, so it is
// fetched once and decides whether the browser check is offered or whether
// pasting a cookie by hand is the only route.
export type SolverCapability = {
  available: boolean;
  reason?: string;
};

// SolveResult is one solve. Captured and verified are separate because they
// answer different questions: captured says material was read, verified says a
// request that was previously blocked now succeeds.
export type SolveResult = {
  origin: string;
  captured: boolean;
  verified: boolean;
  // needsInteraction describes the window: a challenge was on screen and went
  // unanswered. That is an expected middle state, not a failure.
  needsInteraction: boolean;
  // challenge describes the site: it is still refusing requests. It is never true
  // at the same time as verified.
  challenge: boolean;
  // cookies lists the names that were stored. Values are never returned.
  cookies: string[];
  message: string;
};
// SourceChallenge is the per-source view of an outstanding browser check.
export type SourceChallenge = {
  // state is "challenged" when a check can be cleared, or "blocked" when the
  // origin refused access outright.
  state: "challenged" | "blocked";
  // origins lists the domains awaiting clearance. A source whose pages and
  // images sit on different domains reports both.
  origins: string[];
  hits: number;
  lastSeen: number;
  message: string;
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
// One pacing layer of a source's download policy. A null field means that
// layer stays silent and the next one decides it. burst counts concurrent
// chapters, not milliseconds.
export type DownloadPolicyLayer = {
  intervalMs: number | null;
  maxAttempts: number | null;
  backoffMs: number | null;
  burst: number | null;
};
// The three pacing layers for one source: the user's override wins over the
// source's own suggestion, which wins over the host defaults.
export type SourceDownloadsPolicy = {
  override: DownloadPolicyLayer;
  hint: DownloadPolicyLayer;
  defaults: DownloadPolicyLayer;
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
  // pausedSources names the sources whose new downloads are paused; the pause
  // lasts for the daemon's lifetime only.
  pausedSources?: string[];
};
export type DownloadEvent = {
  type: string;
  // Item-less events announce a downloader-level change: "state" carries the
  // paused flag and the counters, "reordered" announces an order change.
  item?: QueueItem;
  stats: DownloadSnapshot["stats"];
  paused: boolean;
  pausedSources?: string[];
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
export type MigrationResponse = {
  manga: Aggregate;
  source: string;
};
export type MigrationSource = { source: Source; count: number; imported?: boolean };

// MigrationStatus is the state of one title in a streaming migration job.
export type MigrationStatus = "searching" | "success" | "notFound" | "failed" | "cancelled";

export type MigrationTitleEvent = {
  mangaId: string;
  title: string;
  status: MigrationStatus;
  source?: Source;
  manga?: Manga;
  score?: number;
  chapterCount?: number;
  latestChapter?: number;
  error?: string;
};

// MigrationFrame is one message on the migration websocket. A snapshot lists
// every title's current state so a reconnecting client can resync; a title
// frame carries one update; complete ends the job.
export type MigrationFrame = {
  type: "snapshot" | "title" | "complete";
  jobId: string;
  done?: boolean;
  title?: MigrationTitleEvent;
  titles?: MigrationTitleEvent[];
};

// MigrationJob identifies a started job. sourceId is the library source the
// job was scoped to, so the client can offer every other plugin for a manual
// search.
export type MigrationJob = { jobId: string; count: number; sourceId?: string };

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
