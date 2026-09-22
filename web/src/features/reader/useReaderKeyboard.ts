// Host keydown listener for the reader chrome. The engine dispatchers own
// the keys in the shared keyboard map (arrows and D/A with RTL inversion, M
// for the HUD); everything else lives here.

import { useEffect } from "react";
import type { Dispatch, SetStateAction } from "react";
import type { Chapter } from "../../types";
import type { ReaderMode, ReaderOverrides } from "./readerSettings";

export interface ReaderKeyboardArgs {
  helpOpen: boolean;
  goToOpen: boolean;
  settingsOpen: boolean;
  drawerOpen: boolean;
  mode: ReaderMode;
  mangaId: string;
  pageCount: number;
  prev?: Chapter;
  next?: Chapter;
  setHelpOpen: Dispatch<SetStateAction<boolean>>;
  setGoToOpen: Dispatch<SetStateAction<boolean>>;
  setSettingsOpen: Dispatch<SetStateAction<boolean>>;
  setDrawerOpen: Dispatch<SetStateAction<boolean>>;
  hideChrome: () => void;
  toggleFullscreen: () => void;
  goChapter: (id: string) => void;
  saveReaderOverride: (patch: Partial<ReaderOverrides>) => void;
  setMode: Dispatch<SetStateAction<ReaderMode>>;
  turnNext: () => void;
  turnPrevious: () => void;
  goToPageIndex: (page: number) => void;
}

export function useReaderKeyboard({
  helpOpen,
  goToOpen,
  settingsOpen,
  drawerOpen,
  mode,
  mangaId,
  pageCount,
  prev,
  next,
  setHelpOpen,
  setGoToOpen,
  setSettingsOpen,
  setDrawerOpen,
  hideChrome,
  toggleFullscreen,
  goChapter,
  saveReaderOverride,
  setMode,
  turnNext,
  turnPrevious,
  goToPageIndex,
}: ReaderKeyboardArgs): void {
  useEffect(() => {
    // Host keys: chapter flow, fullscreen, dialogs, mode cycling, the page
    // extras the engine map cannot express (Space with its shift-for-back
    // rule, PgDn/PgUp, Home/End), and the chrome-hide on the arrows and D/A
    // the view dispatchers navigate with. The hide is unconditional there,
    // matching the turn controls; suppression and editable-focus guards apply
    // to both halves alike.
    const onKey = (event: KeyboardEvent) => {
      // Shortcuts must not fire while a form control has focus: typing into
      // another field or stepping the page slider must stay local to it.
      // Buttons are left alone: arrows never activate a focused button, so a
      // focused control must not trap page navigation.
      const target = event.target as HTMLElement | null;
      if (
        target &&
        (target.tagName === "INPUT" ||
          target.tagName === "TEXTAREA" ||
          target.tagName === "SELECT" ||
          target.isContentEditable)
      ) {
        return;
      }
      if (event.key === "Escape") {
        // Close the topmost layer first: help, go-to, settings, drawer.
        if (helpOpen) setHelpOpen(false);
        else if (goToOpen) setGoToOpen(false);
        else if (settingsOpen) setSettingsOpen(false);
        else if (drawerOpen) setDrawerOpen(false);
        return;
      }
      // An open dialog owns the keyboard: the reader behind it must not turn
      // pages, cycle modes, or hide its own chrome while the user is choosing.
      if (helpOpen || goToOpen || settingsOpen || drawerOpen) return;
      // The view dispatchers navigate on arrows and D/A; the host hides the
      // chrome for them, matching the turn controls. Modified keys belong to
      // the host and the browser, mirroring the dispatcher guards.
      if (!event.ctrlKey && !event.metaKey && !event.altKey) {
        const key = event.key;
        if (
          key === "ArrowRight" ||
          key === "ArrowLeft" ||
          key.toLowerCase() === "d" ||
          key.toLowerCase() === "a"
        ) {
          hideChrome();
        }
      }
      if (event.key.toLowerCase() === "f") toggleFullscreen();
      if (event.key === "?") {
        setHelpOpen(true);
        return;
      }
      if (event.key.toLowerCase() === "g") {
        setGoToOpen((value) => !value);
        return;
      }
      if (event.key.toLowerCase() === "n" && next) {
        goChapter(next.id);
        return;
      }
      if (event.key.toLowerCase() === "p" && prev) {
        goChapter(prev.id);
        return;
      }
      if (event.key.toLowerCase() === "w") {
        // The header quick-switcher and the `w` key share one path: both
        // persist the per-title override, so a late settings fetch can never
        // clobber a mode the reader just chose.
        const nextMode = mode === "single" ? "double" : mode === "double" ? "webtoon" : "single";
        if (mangaId) saveReaderOverride({ mode: nextMode });
        else setMode(nextMode);
      }
      // Paged extras. The webtoon column scrolls natively on Space because
      // the shared map leaves it unbound there; Space on a focused button
      // keeps its native activation.
      if (mode !== "webtoon") {
        if (event.key === " " && target?.tagName !== "BUTTON") {
          event.preventDefault();
          if (event.shiftKey) turnPrevious();
          else turnNext();
        } else if (event.key === "PageDown" || event.key === "PageUp") {
          event.preventDefault();
          if (event.key === "PageDown") turnNext();
          else turnPrevious();
        } else if (event.key === "Home") {
          event.preventDefault();
          goToPageIndex(0);
        } else if (event.key === "End") {
          event.preventDefault();
          goToPageIndex(pageCount - 1);
        }
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [
    drawerOpen,
    next,
    prev,
    goChapter,
    goToOpen,
    goToPageIndex,
    helpOpen,
    hideChrome,
    mangaId,
    mode,
    pageCount,
    saveReaderOverride,
    settingsOpen,
    toggleFullscreen,
    turnNext,
    turnPrevious,
    setHelpOpen,
    setGoToOpen,
    setSettingsOpen,
    setDrawerOpen,
    setMode,
  ]);
}
