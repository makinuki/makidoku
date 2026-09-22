import { Link } from "react-router-dom";
import { ChevronRight } from "lucide-react";
import { PageHeader } from "../../components/States";
import { settingsSections } from "./settingsCatalog";

export function SettingsIndexPage() {
  return (
    <div>
      <PageHeader title="Settings" />
      <div className="overflow-hidden rounded-xl border border-zinc-800 bg-zinc-900/40">
        {settingsSections.map((section) => {
          const Icon = section.icon;
          return (
            <Link
              key={section.id}
              to={`/settings/${section.id}`}
              className="flex items-center gap-4 border-b border-zinc-800 px-4 py-3.5 last:border-b-0 hover:bg-zinc-800/50"
            >
              <Icon size={18} className="shrink-0 text-amber-400" />
              <span className="min-w-0 flex-1">
                <span className="block text-sm font-medium">{section.title}</span>
                <span className="block truncate text-xs text-zinc-500">{section.subtitle}</span>
              </span>
              <ChevronRight size={16} className="shrink-0 text-zinc-600" />
            </Link>
          );
        })}
      </div>
    </div>
  );
}
