import { useEffect, useState, type ReactNode } from "react";
import { NavLink, Link } from "react-router-dom";
import {
  Download,
  History,
  Library,
  Search,
  Settings,
  Wifi,
  WifiOff,
  RefreshCw,
} from "lucide-react";
import { api } from "../api";
import { Logo } from "../components/Logo";
import type { SearchMode } from "../features/search/GlobalSearch";

export function AppShell({
  children,
  onSearch,
  searchMode,
}: {
  children: ReactNode;
  onSearch: () => void;
  searchMode: SearchMode;
}) {
  const [connected, setConnected] = useState<boolean | null>(null);
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
    ["/library", "Library", Library],
    ["/browse", "Browse", Search],
    ["/updates", "Updates", RefreshCw],
    ["/history", "History", History],
    ["/downloads", "Downloads", Download],
  ] as const;
  const searchLabel = searchMode === "settings" ? "Search settings" : "Search library";
  return (
    <div className="min-h-screen bg-zinc-950 text-zinc-100 lg:flex">
      <aside className="border-b border-zinc-800 bg-zinc-900/80 lg:sticky lg:top-0 lg:flex lg:h-screen lg:w-64 lg:flex-col lg:border-b-0 lg:border-r">
        <div className="flex h-16 shrink-0 items-center gap-3 border-b border-zinc-800 px-5">
          <Logo className="size-8 shrink-0" />
          <Link to="/" className="font-semibold tracking-wide">
            MakiDoku
          </Link>
        </div>
        <nav className="flex gap-1 overflow-x-auto p-3 lg:flex-1 lg:flex-col lg:overflow-y-auto">
          {links.map(([to, label, Icon]) => (
            <NavLink
              key={to}
              to={to}
              className={({ isActive }) =>
                `flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm ${isActive ? "bg-zinc-800 text-white" : "text-zinc-400 hover:bg-zinc-800/70 hover:text-white"}`
              }
            >
              <Icon size={17} /> <span>{label}</span>
            </NavLink>
          ))}
        </nav>
        <div className="border-t border-zinc-800 p-3 lg:shrink-0">
          <NavLink
            to="/settings"
            className={({ isActive }) =>
              `flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm ${isActive ? "bg-zinc-800 text-white" : "text-zinc-400 hover:bg-zinc-800/70 hover:text-white"}`
            }
          >
            <Settings size={17} /> Settings
          </NavLink>
        </div>
      </aside>
      <div className="min-w-0 flex-1">
        <header className="sticky top-0 z-20 flex h-16 items-center gap-4 border-b border-zinc-800 bg-zinc-950/90 px-4 backdrop-blur lg:px-8">
          <button
            onClick={onSearch}
            className="flex min-w-0 flex-1 items-center gap-2 rounded-lg border border-zinc-800 bg-zinc-900 px-3 py-2 text-left text-sm text-zinc-400 hover:border-zinc-700"
            aria-label={searchLabel}
          >
            <Search size={16} />
            <span className="truncate">{searchLabel}...</span>
            <kbd className="ml-auto hidden rounded border border-zinc-700 px-1.5 py-0.5 text-[10px] text-zinc-500 sm:inline">
              Ctrl K
            </kbd>
          </button>
          <span
            className={`flex items-center gap-2 text-xs ${connected ? "text-emerald-400" : connected === false ? "text-red-400" : "text-zinc-500"}`}
            aria-label="Daemon status"
          >
            {connected ? <Wifi size={15} /> : <WifiOff size={15} />}
            <span className="hidden sm:inline">
              {connected ? "Connected" : connected === false ? "Offline" : "Checking"}
            </span>
          </span>
        </header>
        <main>{children}</main>
      </div>
    </div>
  );
}
