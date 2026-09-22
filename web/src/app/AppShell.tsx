import { useEffect, useState, type MouseEvent, type ReactNode } from "react";
import { NavLink, Link, useLocation } from "react-router-dom";
import {
  Download,
  EyeOff,
  History,
  Library,
  BarChart3,
  Search,
  Settings,
  Wifi,
  WifiOff,
  RefreshCw,
} from "lucide-react";
import { api } from "../api";
import { Logo } from "../components/Logo";
import type { SearchMode } from "../features/search/GlobalSearch";
import { BottomBar, TabletRail } from "./MobileNav";
import { onBottomNavVisibility, useNavBadges } from "./nav";

export function AppShell({
  children,
  onSearch,
  searchMode,
}: {
  children: ReactNode;
  onSearch: () => void;
  searchMode: SearchMode;
}) {
  const location = useLocation();
  const [connected, setConnected] = useState<boolean | null>(null);
  const [incognito, setIncognito] = useState(false);
  const [navVisible, setNavVisible] = useState(true);
  const { updates, pluginUpdates } = useNavBadges();
  useEffect(() => {
    let active = true;
    api
      .incognito()
      .then((state) => active && setIncognito(state.enabled))
      .catch(() => {
        // Incognito state is optional; the default stays off if the read fails.
      });
    return () => {
      active = false;
    };
  }, []);
  useEffect(() => onBottomNavVisibility(setNavVisible), []);
  const toggleIncognito = () => {
    const next = !incognito;
    setIncognito(next);
    void api.setIncognito(next).catch(() => setIncognito(!next));
  };
  useEffect(() => {
    let active = true;
    const check = () =>
      api
        .health()
        .then(() => active && setConnected(true))
        .catch(() => active && setConnected(false));
    check();
    const timer = window.setInterval(check, 15000);
    return () => {
      active = false;
      window.clearInterval(timer);
    };
  }, []);
  const links = [
    ["/library", "Library", Library, 0],
    ["/browse", "Browse", Search, pluginUpdates],
    ["/updates", "Updates", RefreshCw, updates],
    ["/history", "History", History, 0],
    ["/stats", "Statistics", BarChart3, 0],
    ["/downloads", "Downloads", Download, 0],
  ] as const;
  const searchLabel = searchMode === "settings" ? "Search settings" : "Search library";
  // The reader owns the full viewport; the tab bar and rail stay out of its way.
  const immersive = location.pathname.startsWith("/reader");
  const scrollTop = (to: string) => (event: MouseEvent) => {
    if (location.pathname !== to) return;
    // Clicking the active desktop entry never navigates; it returns to the
    // top of the current list instead.
    event.preventDefault();
    window.scrollTo({ top: 0, behavior: "smooth" });
  };
  return (
    <div className="min-h-screen bg-zinc-950 text-zinc-100 lg:flex">
      <aside className="hidden border-b border-zinc-800 bg-zinc-900/80 lg:sticky lg:top-0 lg:flex lg:h-screen lg:w-64 lg:flex-col lg:border-b-0 lg:border-r">
        <div className="flex h-16 shrink-0 items-center gap-3 border-b border-zinc-800 px-5">
          <Logo className="size-8 shrink-0" />
          <Link to="/" className="font-semibold tracking-wide">
            MakiDoku
          </Link>
        </div>
        <nav className="flex gap-1 overflow-x-auto p-3 lg:flex-1 lg:flex-col lg:overflow-y-auto">
          {links.map(([to, label, Icon, badge]) => (
            <NavLink
              key={to}
              to={to}
              onClick={scrollTop(to)}
              className={({ isActive }) =>
                `flex min-h-11 items-center gap-3 rounded-lg px-3 py-2.5 text-sm ${isActive ? "bg-zinc-800 text-white" : "text-zinc-400 hover:bg-zinc-800/70 hover:text-white active:bg-zinc-800/70 active:text-white"}`
              }
            >
              <Icon size={17} /> <span className="flex-1">{label}</span>
              {badge > 0 && (
                <span
                  role="status"
                  aria-label={`${badge} pending`}
                  className="flex min-w-5 items-center justify-center rounded-full bg-amber-400 px-1 text-[10px] font-semibold leading-4 text-zinc-950"
                >
                  {badge > 99 ? "99+" : badge}
                </span>
              )}
            </NavLink>
          ))}
        </nav>
        <div className="border-t border-zinc-800 p-3 lg:shrink-0">
          <NavLink
            to="/settings"
            onClick={scrollTop("/settings")}
            className={({ isActive }) =>
              `flex min-h-11 items-center gap-3 rounded-lg px-3 py-2.5 text-sm ${isActive ? "bg-zinc-800 text-white" : "text-zinc-400 hover:bg-zinc-800/70 hover:text-white active:bg-zinc-800/70 active:text-white"}`
            }
          >
            <Settings size={17} /> Settings
          </NavLink>
        </div>
      </aside>
      {!immersive && (
        <>
          <TabletRail
            onSearch={onSearch}
            visible={navVisible}
            updates={updates}
            pluginUpdates={pluginUpdates}
          />
          <BottomBar
            onSearch={onSearch}
            visible={navVisible}
            updates={updates}
            pluginUpdates={pluginUpdates}
          />
        </>
      )}
      <div className="min-w-0 flex-1 md:pl-[76px] lg:pl-0">
        <header className="sticky top-0 z-20 flex h-16 items-center gap-3 border-b border-zinc-800 bg-zinc-950/90 px-4 backdrop-blur lg:px-8">
          <Logo className="size-7 shrink-0 md:hidden" />
          <button
            onClick={onSearch}
            className="flex min-h-11 min-w-0 flex-1 items-center gap-2 rounded-lg border border-zinc-800 bg-zinc-900 px-3 py-2 text-left text-sm text-zinc-400 hover:border-zinc-700 active:border-zinc-700"
            aria-label={searchLabel}
          >
            <Search size={16} />
            <span className="truncate">{searchLabel}...</span>
            <kbd className="ml-auto hidden rounded border border-zinc-700 px-1.5 py-0.5 text-[10px] text-zinc-500 sm:inline">
              Ctrl K
            </kbd>
          </button>
          <button
            type="button"
            onClick={toggleIncognito}
            aria-pressed={incognito}
            aria-label={incognito ? "Turn off incognito mode" : "Turn on incognito mode"}
            title={incognito ? "Incognito mode is on" : "Incognito mode is off"}
            className={`flex min-h-11 min-w-11 items-center justify-center rounded-lg border p-2 ${incognito ? "border-amber-400/60 bg-amber-400/10 text-amber-300" : "border-zinc-800 text-zinc-400 hover:border-zinc-700 active:border-zinc-700"}`}
          >
            <EyeOff size={16} />
          </button>
          <span
            className={`flex items-center gap-2 text-xs ${connected ? "text-emerald-400" : connected === false ? "text-red-400" : "text-zinc-500"}`}
            aria-label="Server status"
          >
            {connected ? <Wifi size={15} /> : <WifiOff size={15} />}
            <span className="hidden sm:inline">
              {connected ? "Connected" : connected === false ? "Offline" : "Checking"}
            </span>
          </span>
        </header>
        {incognito && (
          <div className="flex items-center gap-2 border-b border-amber-400/40 bg-amber-400/10 px-4 py-2 text-xs text-amber-200 lg:px-8">
            <EyeOff size={14} /> Incognito mode is on. Reading activity will not be saved.
          </div>
        )}
        <main
          className={
            immersive ? undefined : "pb-[calc(var(--nav-height)+var(--sat-bottom))] md:pb-0"
          }
        >
          {children}
        </main>
      </div>
    </div>
  );
}
