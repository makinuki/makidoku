// Reading-progress pipeline for one chapter: incognito gating, the debounced
// progress write with session-time accrual, the bounded offline retry queue,
// and the exit flush on tab hide, close, reconnect, and chapter switch.

import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "../../api";
import type { Aggregate } from "../../types";

type PendingProgress = {
  mangaId: string;
  chapterId: string;
  page: number;
  total: number;
  complete: boolean;
  sessionSeconds: number;
};

export interface UseReaderProgressArgs {
  mangaId: string;
  chapterId: string;
  aggregate: Aggregate | undefined;
  pageCount: number;
  spreadEndForPage: (pageIndex: number) => number;
  // Live reading position for the exit flush: the engine position while it
  // exists, otherwise the resume fallback.
  currentPageIndex: () => number;
}

export function useReaderProgress({
  mangaId,
  chapterId,
  aggregate,
  pageCount,
  spreadEndForPage,
  currentPageIndex,
}: UseReaderProgressArgs): {
  incognito: boolean;
  saveAtPage: (pageIndex: number) => void;
  resetReadingSession: () => void;
} {
  const [incognito, setIncognito] = useState(false);
  const incognitoRef = useRef(false);
  // Progress writes are blocked until the incognito state has been read once;
  // a sample can otherwise land before the fetch resolves and leak a history
  // write for a session the user believes is incognito.
  const incognitoLoadedRef = useRef(false);
  const sessionStartedAt = useRef(Date.now());
  // Whole seconds not yet reported; sub-second remainders carry over so a save
  // per page turn cannot truncate the session away.
  const pendingSeconds = useRef(0);
  const saver = useRef<(() => void) | null>(null);
  const retryQueue = useRef<PendingProgress[]>([]);
  // Last progress write, to drop saves that carry no new information: the
  // engine samples immediately on every discrete move, so without this the
  // exit flush would re-post the position a page turn just recorded.
  const lastPostedRef = useRef<{ key: string; page: number } | null>(null);

  useEffect(() => {
    incognitoRef.current = incognito;
  }, [incognito]);

  useEffect(() => {
    let active = true;
    api
      .incognito()
      .then((state) => {
        if (!active) return;
        incognitoRef.current = state.enabled;
        setIncognito(state.enabled);
        incognitoLoadedRef.current = true;
      })
      .catch(() => {
        // Incognito state is optional; a failed read fails closed so no
        // progress leaves the device until a later mount can confirm it.
        incognitoRef.current = true;
        incognitoLoadedRef.current = true;
      });
    return () => {
      active = false;
    };
  }, []);

  const postProgress = useCallback((entry: PendingProgress) => {
    // Incognito never leaves the device: the daemon also null-writes, but
    // the frontend must not emit the request in the first place. Until the
    // incognito state has been read once, unknown is treated as enabled.
    if (!incognitoLoadedRef.current || incognitoRef.current) return Promise.resolve();
    return api
      .progress(
        entry.mangaId,
        entry.chapterId,
        entry.page,
        entry.total,
        entry.complete,
        entry.sessionSeconds,
      )
      .catch((e) => {
        // Offline or transient failure queues for the next tick instead of
        // dropping the position; the queue is bounded and oldest-first.
        retryQueue.current = [...retryQueue.current, entry].slice(-5);
        console.error("saving reading progress failed", e);
      });
  }, []);

  const flushProgress = useCallback(() => {
    const queued = retryQueue.current;
    retryQueue.current = [];
    return (async () => {
      for (const entry of queued) await postProgress(entry);
      if (saver.current) {
        const save = saver.current;
        saver.current = null;
        save();
      }
    })();
  }, [postProgress]);

  const saveAtPage = useCallback(
    (pageIndex: number) => {
      if (!pageCount || !aggregate) return;
      const visibleEnd = spreadEndForPage(pageIndex);
      const now = Date.now();
      // Reading time accrues between writes. Most intervals cover well under
      // a second; flooring each one on its own would drop them all. Whole
      // seconds are reported and the remainder stays pending. The daemon
      // stores one reading session per write, so the value is the delta since
      // the previous write, capped at 300 seconds.
      pendingSeconds.current = Math.min(
        300,
        pendingSeconds.current + (now - sessionStartedAt.current) / 1000,
      );
      sessionStartedAt.current = now;
      const seconds = Math.floor(pendingSeconds.current);
      pendingSeconds.current -= seconds;
      const key = `${mangaId}/${chapterId}`;
      const last = lastPostedRef.current;
      if (last && last.key === key && last.page === visibleEnd && seconds === 0) return;
      lastPostedRef.current = { key, page: visibleEnd };
      void postProgress({
        mangaId,
        chapterId,
        page: visibleEnd,
        total: pageCount,
        complete: visibleEnd >= pageCount,
        sessionSeconds: seconds,
      });
    },
    [pageCount, aggregate, mangaId, chapterId, postProgress, spreadEndForPage],
  );

  // A new chapter starts a fresh reading session: the timer and the pending
  // sub-second remainder must not carry over from the previous chapter.
  const resetReadingSession = useCallback(() => {
    sessionStartedAt.current = Date.now();
    pendingSeconds.current = 0;
  }, []);

  useEffect(() => {
    // Discrete engine moves sample immediately (same-page scroll samples
    // throttle to 1s inside the engine), so persistence rides
    // onPositionSample; the pending write below only covers
    // unload and chapter switch.
    saver.current = () => {
      saveAtPage(currentPageIndex());
    };
  }, [saveAtPage, currentPageIndex]);

  useEffect(() => {
    // A pending write is flushed when leaving the reader, switching
    // chapters, hiding the tab, or closing the page so the final position is
    // recorded even when nothing sampled after the last move.
    const flush = () => {
      void flushProgress();
    };
    window.addEventListener("pagehide", flush);
    document.addEventListener("visibilitychange", flush);
    window.addEventListener("online", flush);
    return () => {
      window.removeEventListener("pagehide", flush);
      document.removeEventListener("visibilitychange", flush);
      window.removeEventListener("online", flush);
      if (saver.current) {
        saver.current();
        saver.current = null;
      }
    };
  }, [mangaId, chapterId, flushProgress]);

  return { incognito, saveAtPage, resetReadingSession };
}
