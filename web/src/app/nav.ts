import { useEffect, useState } from "react";
import { Ellipsis, History, Library, RefreshCw, Search } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { api } from "../api";

export type MobileTabId = "library" | "browse" | "updates" | "history" | "more";

export type MobileTab = {
  id: MobileTabId;
  to: string;
  label: string;
  Icon: LucideIcon;
  matches: (pathname: string) => boolean;
};

export const mobileTabs: MobileTab[] = [
  {
    id: "library",
    to: "/library",
    label: "Library",
    Icon: Library,
    matches: (pathname) => pathname === "/" || pathname.startsWith("/library"),
  },
  {
    id: "browse",
    to: "/browse",
    label: "Browse",
    Icon: Search,
    matches: (pathname) => pathname.startsWith("/browse"),
  },
  {
    id: "updates",
    to: "/updates",
    label: "Updates",
    Icon: RefreshCw,
    matches: (pathname) => pathname.startsWith("/updates"),
  },
  {
    id: "history",
    to: "/history",
    label: "History",
    Icon: History,
    matches: (pathname) => pathname.startsWith("/history"),
  },
  {
    id: "more",
    to: "/more",
    label: "More",
    Icon: Ellipsis,
    matches: (pathname) => pathname.startsWith("/more"),
  },
];

// Tapping the active bottom-bar or rail tab re-runs that tab's shortcut
// instead of navigating. Pages subscribe to the tab they own; navigation
// shortcuts run in the nav component itself.
export const RESELECT_EVENT = "makidoku:reselect";
export const BOTTOM_NAV_EVENT = "makidoku:bottomnav";
export const BADGES_EVENT = "makidoku:badges-refresh";

export function dispatchReselect(tab: MobileTabId) {
  window.dispatchEvent(new CustomEvent(RESELECT_EVENT, { detail: { tab } }));
}

export function onReselect(handler: (tab: MobileTabId) => void) {
  const listener = (event: Event) => {
    const tab = (event as CustomEvent<{ tab: MobileTabId }>).detail?.tab;
    if (tab) handler(tab);
  };
  window.addEventListener(RESELECT_EVENT, listener);
  return () => window.removeEventListener(RESELECT_EVENT, listener);
}

// Selection modes hide the bottom bar and rail so the bulk action bar owns
// the bottom edge, matching the native action-mode behavior.
export function setBottomNavVisible(visible: boolean) {
  window.dispatchEvent(new CustomEvent(BOTTOM_NAV_EVENT, { detail: { visible } }));
}

export function onBottomNavVisibility(handler: (visible: boolean) => void) {
  const listener = (event: Event) => {
    handler((event as CustomEvent<{ visible: boolean }>).detail?.visible ?? true);
  };
  window.addEventListener(BOTTOM_NAV_EVENT, listener);
  return () => window.removeEventListener(BOTTOM_NAV_EVENT, listener);
}

export function refreshBadges() {
  window.dispatchEvent(new CustomEvent(BADGES_EVENT));
}

export function historyResumeTarget(
  events: { manga: { id: string }; chapter?: { id: string }; page?: number | null }[],
) {
  const latest = events.find((event) => event.chapter);
  if (!latest?.chapter) return "/history";
  const page = latest.page != null ? `?page=${latest.page}` : "";
  return `/reader/${encodeURIComponent(latest.manga.id)}/${encodeURIComponent(latest.chapter.id)}${page}`;
}

// Jump back into the most recently read chapter. Falls back to the history
// list when nothing readable is stored or the lookup fails.
export async function resumeLastRead(navigate: (to: string) => void) {
  try {
    navigate(historyResumeTarget(await api.history()));
  } catch {
    navigate("/history");
  }
}

export function useNavBadges() {
  const [updates, setUpdates] = useState(0);
  const [pluginUpdates, setPluginUpdates] = useState(0);
  useEffect(() => {
    let active = true;
    const load = () => {
      void Promise.allSettled([api.updates(), api.catalog()]).then(([pending, catalog]) => {
        if (!active) return;
        if (pending.status === "fulfilled") setUpdates(pending.value.length);
        if (catalog.status === "fulfilled") {
          setPluginUpdates(
            catalog.value.filter((entry) => entry.installed && entry.updateAvailable).length,
          );
        }
      });
    };
    load();
    const timer = window.setInterval(load, 120000);
    window.addEventListener(BADGES_EVENT, load);
    return () => {
      active = false;
      window.clearInterval(timer);
      window.removeEventListener(BADGES_EVENT, load);
    };
  }, []);
  return { updates, pluginUpdates };
}
