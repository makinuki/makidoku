import { describe, expect, it } from "vite-plus/test";
import {
  chapterLabelFor,
  nextUnreadChapterId,
  orderChaptersForReading,
  prevNextChapter,
  resumeChapterId,
} from "./chapters";
import type { Chapter } from "../../../types";

const chapter = (id: string, overrides: Partial<Chapter> = {}): Chapter => ({
  id,
  mangaId: "manga",
  downloaded: false,
  ...overrides,
});

describe("chapter reading order", () => {
  it("sorts oldest-first with specials at the end", () => {
    const ordered = orderChaptersForReading([
      chapter("c10", { chapterNumber: 10.5 }),
      chapter("special", { title: "Extra" }),
      chapter("c2", { chapterNumber: 2 }),
      chapter("c1", { chapterNumber: 1 }),
    ]);
    expect(ordered.map((item) => item.id)).toEqual(["c1", "c2", "c10", "special"]);
  });

  it("finds the neighbors of the current chapter", () => {
    const chapters = [
      chapter("c1", { chapterNumber: 1 }),
      chapter("c2", { chapterNumber: 2 }),
      chapter("c3", { chapterNumber: 3 }),
    ];
    expect(prevNextChapter("c2", chapters).prev?.id).toBe("c1");
    expect(prevNextChapter("c2", chapters).next?.id).toBe("c3");
    expect(prevNextChapter("c1", chapters).prev).toBeUndefined();
    expect(prevNextChapter("c3", chapters).next).toBeUndefined();
    expect(prevNextChapter("missing", chapters).next).toBeUndefined();
  });

  it("resumes saved progress, else the first unread chapter", () => {
    const chapters = [
      chapter("c1", { chapterNumber: 1, read: true }),
      chapter("c2", { chapterNumber: 2 }),
      chapter("c3", { chapterNumber: 3 }),
    ];
    expect(resumeChapterId({ progress: { lastReadChapterId: "c3" } }, chapters)).toBe("c3");
    expect(resumeChapterId({ progress: null }, chapters)).toBe("c2");
    expect(resumeChapterId({}, [])).toBe("");
    expect(
      resumeChapterId(
        { progress: { lastReadChapterId: "gone" } },
        chapters.map((item) => ({ ...item, read: true })),
      ),
    ).toBe("c1");
  });

  it("finds the next unread chapter and labels chapters", () => {
    expect(nextUnreadChapterId([chapter("c1", { chapterNumber: 1, read: true })])).toBe("");
    expect(chapterLabelFor(chapter("x", { chapterNumber: 2, volume: 3 }))).toBe(
      "Vol. 3 · Chapter 2",
    );
    expect(chapterLabelFor(chapter("x", { title: "Extra" }))).toBe("Extra");
    expect(chapterLabelFor(undefined)).toBe("Chapter");
  });
});
