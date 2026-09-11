import { SectionPage, SettingRows, SettingsCard } from "../SettingsPrimitives";

export function AppearanceSection() {
  return (
    <SectionPage section="appearance">
      <SettingsCard title="Timestamps" description="How times and dates are shown across the app.">
        <SettingRows section="appearance" />
      </SettingsCard>
    </SectionPage>
  );
}
