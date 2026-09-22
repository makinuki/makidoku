import { Link, useLocation, useNavigate } from "react-router-dom";
import type { MouseEvent } from "react";
import { Logo } from "../components/Logo";
import { dispatchReselect, mobileTabs, resumeLastRead, type MobileTab } from "./nav";

function CountBadge({ count, label }: { count: number; label: string }) {
  if (count <= 0) return null;
  return (
    <span
      role="status"
      aria-label={`${count > 99 ? "99+" : count} ${label}`}
      className="absolute right-1/2 top-1 flex min-w-5 translate-x-6 items-center justify-center rounded-full bg-amber-400 px-1 text-[10px] font-semibold leading-4 text-zinc-950"
    >
      {count > 99 ? "99+" : count}
    </span>
  );
}

function useReselect(onSearch: () => void) {
  const navigate = useNavigate();
  return (tab: MobileTab, active: boolean, event: MouseEvent) => {
    if (!active) return;
    // Tapping the active tab repeats its shortcut; plain navigation would
    // reload the same page and lose scroll position.
    event.preventDefault();
    switch (tab.id) {
      case "library":
        dispatchReselect("library");
        break;
      case "updates":
        navigate("/downloads");
        break;
      case "history":
        void resumeLastRead(navigate);
        break;
      case "browse":
        onSearch();
        break;
      case "more":
        navigate("/settings");
        break;
    }
  };
}

function BarTab({
  tab,
  active,
  badge,
  badgeLabel,
  onTap,
}: {
  tab: MobileTab;
  active: boolean;
  badge: number;
  badgeLabel: string;
  onTap: (tab: MobileTab, active: boolean, event: MouseEvent) => void;
}) {
  const Icon = tab.Icon;
  return (
    <Link
      to={tab.to}
      aria-current={active ? "page" : undefined}
      onClick={(event) => onTap(tab, active, event)}
      className={`relative flex min-h-16 flex-col items-center justify-center gap-1 text-[11px] font-medium ${
        active ? "text-amber-300" : "text-zinc-400 hover:text-zinc-200 active:text-zinc-200"
      }`}
    >
      <Icon size={21} aria-hidden="true" />
      <CountBadge count={badge} label={badgeLabel} />
      <span>{tab.label}</span>
    </Link>
  );
}

export function BottomBar({
  onSearch,
  visible,
  updates,
  pluginUpdates,
}: {
  onSearch: () => void;
  visible: boolean;
  updates: number;
  pluginUpdates: number;
}) {
  const location = useLocation();
  const onTap = useReselect(onSearch);
  return (
    <nav
      aria-label="Primary"
      className={`fixed inset-x-0 bottom-0 z-30 border-t border-zinc-800 bg-zinc-900/95 backdrop-blur transition-transform duration-200 md:hidden ${
        visible ? "" : "translate-y-full"
      }`}
    >
      <div className="grid grid-cols-5 pb-[env(safe-area-inset-bottom,0px)]">
        {mobileTabs.map((tab) => (
          <BarTab
            key={tab.id}
            tab={tab}
            active={tab.matches(location.pathname)}
            badge={tab.id === "updates" ? updates : tab.id === "more" ? pluginUpdates : 0}
            badgeLabel={tab.id === "updates" ? "pending updates" : "pending plugin updates"}
            onTap={onTap}
          />
        ))}
      </div>
    </nav>
  );
}

export function TabletRail({
  onSearch,
  visible,
  updates,
  pluginUpdates,
}: {
  onSearch: () => void;
  visible: boolean;
  updates: number;
  pluginUpdates: number;
}) {
  const location = useLocation();
  const onTap = useReselect(onSearch);
  return (
    <nav
      aria-label="Primary"
      className={`fixed bottom-0 left-0 top-0 z-30 hidden w-[76px] flex-col items-stretch gap-1 border-r border-zinc-800 bg-zinc-900/95 px-2 py-3 transition-transform duration-200 md:flex lg:hidden ${
        visible ? "" : "-translate-x-full"
      }`}
    >
      <div className="mb-2 flex justify-center">
        <Logo className="size-9" />
      </div>
      {mobileTabs.map((tab) => (
        <BarTab
          key={tab.id}
          tab={tab}
          active={tab.matches(location.pathname)}
          badge={tab.id === "updates" ? updates : tab.id === "more" ? pluginUpdates : 0}
          badgeLabel={tab.id === "updates" ? "pending updates" : "pending plugin updates"}
          onTap={onTap}
        />
      ))}
    </nav>
  );
}
