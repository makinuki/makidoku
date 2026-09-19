import type { Chapter } from "../../../types";

// Reading order is oldest-first: ascending chapter number, unnumbered
// specials at the end. Details screens sort newest-first for browsing, so
// every chapter-flow entry point shares this ordering instead of reusing the
// display sort.
export function orderChaptersForReading(chapters: Chapter[]): Chapter[] {
  return [...chapters].sort((a, b) => {
    if (a.chapterNumber == null && b.chapterNumber == null) return 0;
    if (a.chapterNumber == null) return 1;
    if (b.chapterNumber == null) return -1;
    return a.chapterNumber - b.chapterNumber;
  });
}

export function prevNextChapter(
  currentId: string,
  chapters: Chapter[],
): { prev?: Chapter; next?: Chapter; ordered: Chapter[] } {
  const ordered = orderChaptersForReading(chapters);
  const at = ordered.findIndex((chapter) => chapter.id === currentId);
  if (at < 0) return { ordered };
  return {
    prev: at > 0 ? ordered[at - 1] : undefined,
    next: at < ordered.length - 1 ? ordered[at + 1] : undefined,
    ordered,
  };
}

// Picks where reading starts: the saved chapter while it still exists,
// otherwise the first unread chapter in reading order, otherwise the first
// chapter. An empty list yields "" so callers can disable entry points.
export function resumeChapterId(
  data: { progress?: { lastReadChapterId?: string } | null },
  chapters: Chapter[],
): string {
  const ordered = orderChaptersForReading(chapters);
  if (!ordered.length) return "";
  const saved = data.progress?.lastReadChapterId;
  if (saved && ordered.some((chapter) => chapter.id === saved)) return saved;
  return ordered.find((chapter) => !chapter.read)?.id ?? ordered[0].id;
}

export function nextUnreadChapterId(chapters: Chapter[]): string {
  const ordered = orderChaptersForReading(chapters);
  return ordered.find((chapter) => !chapter.read)?.id ?? "";
}

export function chapterLabelFor(chapter: Chapter | undefined): string {
  if (!chapter) return "Chapter";
  if (chapter.chapterNumber == null) return chapter.title || "Special";
  const base = `Chapter ${chapter.chapterNumber}`;
  return chapter.volume == null ? base : `Vol. ${chapter.volume} · ${base}`;
}
