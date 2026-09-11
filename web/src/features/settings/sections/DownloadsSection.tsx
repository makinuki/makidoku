import { SectionPage, SettingRows, SettingsCard } from "../SettingsPrimitives";

export function DownloadsSection() {
  return (
    <SectionPage section="downloads">
      <SettingsCard title="Queue" description="How new chapters are fetched and stored.">
        <SettingRows section="downloads" />
      </SettingsCard>
    </SectionPage>
  );
}
