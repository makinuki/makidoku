import { useState } from "react";
import type { ReactNode } from "react";
import { Check } from "lucide-react";
import { Modal } from "../../components/Modal";
import {
  cardSizeLabels,
  cardSizes,
  librarySorts,
  naturalDirection,
  readStateLabels,
  readStates,
  sortLabels,
  statusLabels,
  statuses,
  type LibraryFilters,
  type LibrarySourceOption,
  type LibraryView,
} from "./libraryState";

type Tab = "filter" | "sort" | "display";

const tabs: Array<[Tab, string]> = [
  ["filter", "Filter"],
  ["sort", "Sort"],
  ["display", "Display"],
];

function toggle<T>(values: T[], value: T): T[] {
  return values.includes(value) ? values.filter((item) => item !== value) : [...values, value];
}

export function LibraryFiltersModal({
  view,
  sources,
  onPatch,
  onClose,
}: {
  view: LibraryView;
  sources: LibrarySourceOption[];
  onPatch: (patch: Partial<LibraryView>) => void;
  onClose: () => void;
}) {
  const [tab, setTab] = useState<Tab>("filter");
  const patchFilters = (filters: Partial<LibraryFilters>) =>
    onPatch({ filters: { ...view.filters, ...filters } });
  const filtersActive =
    view.filters.readState.length + view.filters.status.length + view.filters.sources.length > 0;
  return (
    <Modal title="Library layout" onClose={onClose}>
      <div className="mb-5 flex gap-1 rounded-xl border border-zinc-800 bg-zinc-950/60 p-1">
        {tabs.map(([id, label]) => (
          <button
            key={id}
            type="button"
            aria-pressed={tab === id}
            onClick={() => setTab(id)}
            className={`flex-1 rounded-lg px-3 py-2 text-sm font-medium ${
              tab === id ? "bg-zinc-800 text-white" : "text-zinc-400 hover:text-white"
            }`}
          >
            {label}
          </button>
        ))}
      </div>
      {tab === "filter" && (
        <div className="grid gap-5">
          <FilterGroup title="Read state">
            {readStates.map((value) => (
              <Choice
                key={value}
                active={view.filters.readState.includes(value)}
                onClick={() => patchFilters({ readState: toggle(view.filters.readState, value) })}
              >
                {readStateLabels[value]}
              </Choice>
            ))}
          </FilterGroup>
          <FilterGroup title="Publication status">
            {statuses.map((value) => (
              <Choice
                key={value}
                active={view.filters.status.includes(value)}
                onClick={() => patchFilters({ status: toggle(view.filters.status, value) })}
              >
                {statusLabels[value] ?? value}
              </Choice>
            ))}
          </FilterGroup>
          {sources.length > 0 && (
            <FilterGroup title="Plugin">
              {sources.map((source) => (
                <Choice
                  key={source.id}
                  active={view.filters.sources.includes(source.id)}
                  onClick={() => patchFilters({ sources: toggle(view.filters.sources, source.id) })}
                >
                  {source.name}
                </Choice>
              ))}
            </FilterGroup>
          )}
          <button
            type="button"
            disabled={!filtersActive}
            onClick={() => patchFilters({ readState: [], status: [], sources: [] })}
            className="justify-self-start rounded-lg border border-zinc-700 px-3 py-2 text-sm text-zinc-300 hover:border-zinc-500 disabled:opacity-40"
          >
            Clear filters
          </button>
        </div>
      )}
      {tab === "sort" && (
        <div className="grid gap-4">
          <div className="grid gap-1">
            {librarySorts.map((value) => (
              <SortRow
                key={value}
                label={sortLabels[value]}
                active={view.sort === value}
                onClick={() => onPatch({ sort: value, direction: naturalDirection(value) })}
              />
            ))}
          </div>
          <div className="flex items-center justify-between gap-3 rounded-lg border border-zinc-800 px-3 py-2.5">
            <span className="text-sm">Direction</span>
            <div className="flex gap-1">
              <Choice
                active={view.direction === "asc"}
                onClick={() => onPatch({ direction: "asc" })}
              >
                Ascending
              </Choice>
              <Choice
                active={view.direction === "desc"}
                onClick={() => onPatch({ direction: "desc" })}
              >
                Descending
              </Choice>
            </div>
          </div>
        </div>
      )}
      {tab === "display" && (
        <div className="grid gap-4">
          <div className="rounded-lg border border-zinc-800 px-3 py-2.5">
            <span className="mb-2 block text-sm">Card size</span>
            <div className="flex gap-1">
              {cardSizes.map((value) => (
                <Choice
                  key={value}
                  active={view.cardSize === value}
                  onClick={() => onPatch({ cardSize: value })}
                >
                  {cardSizeLabels[value]}
                </Choice>
              ))}
            </div>
          </div>
          <ToggleRow
            label="Unread badge"
            description="Show the number of unread chapters on each card."
            active={view.unreadBadge}
            onClick={() => onPatch({ unreadBadge: !view.unreadBadge })}
          />
          <ToggleRow
            label="Reading progress"
            description="Show how far the current chapter has been read."
            active={view.progressBar}
            onClick={() => onPatch({ progressBar: !view.progressBar })}
          />
          <ToggleRow
            label="Continue action"
            description="Offer a shortcut to the last read chapter."
            active={view.continueButton}
            onClick={() => onPatch({ continueButton: !view.continueButton })}
          />
        </div>
      )}
    </Modal>
  );
}

function FilterGroup({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div>
      <h3 className="mb-2 text-xs font-semibold uppercase tracking-wide text-zinc-500">{title}</h3>
      <div className="flex flex-wrap gap-1.5">{children}</div>
    </div>
  );
}

function Choice({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      aria-pressed={active}
      onClick={onClick}
      className={`rounded-full border px-3 py-1.5 text-xs font-medium ${
        active
          ? "border-amber-400 bg-amber-400 text-zinc-950"
          : "border-zinc-700 text-zinc-300 hover:border-zinc-500"
      }`}
    >
      {children}
    </button>
  );
}

function SortRow({
  label,
  active,
  onClick,
}: {
  label: string;
  active: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      aria-pressed={active}
      onClick={onClick}
      className={`flex items-center gap-3 rounded-lg px-3 py-2 text-left text-sm ${
        active ? "bg-zinc-800 text-white" : "text-zinc-300 hover:bg-zinc-800/70"
      }`}
    >
      <span className="flex-1">{label}</span>
      {active && <Check size={15} className="text-amber-400" />}
    </button>
  );
}

function ToggleRow({
  label,
  description,
  active,
  onClick,
}: {
  label: string;
  description: string;
  active: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={active}
      onClick={onClick}
      className="flex items-center justify-between gap-4 rounded-lg border border-zinc-800 px-3 py-2.5 text-left hover:border-zinc-600"
    >
      <span>
        <span className="block text-sm">{label}</span>
        <span className="block text-xs text-zinc-500">{description}</span>
      </span>
      <span
        className={`h-5 w-9 shrink-0 rounded-full p-0.5 ${active ? "bg-amber-400" : "bg-zinc-700"}`}
      >
        <span
          className={`block size-4 rounded-full bg-white transition ${active ? "translate-x-4" : ""}`}
        />
      </span>
    </button>
  );
}
