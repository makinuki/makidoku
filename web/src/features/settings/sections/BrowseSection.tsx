import { Link } from "react-router-dom";
import { SectionPage, SettingRows, SettingsCard } from "../SettingsPrimitives";

export function BrowseSection() {
  return (
    <SectionPage section="browse">
      <SettingsCard title="Sources" description="What the browse catalog shows.">
        <SettingRows section="browse" />
      </SettingsCard>
      <SettingsCard
        title="Plugins"
        description="Installation, updates, removal, and browser clearance are managed from Browse."
      >
        <Link
          to="/browse?tab=plugins"
          className="inline-flex rounded-lg border border-zinc-700 px-3 py-2 text-sm text-zinc-200 hover:border-zinc-500"
        >
          Manage plugins
        </Link>
      </SettingsCard>
    </SectionPage>
  );
}
