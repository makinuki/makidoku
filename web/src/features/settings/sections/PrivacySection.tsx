import { SectionPage, SettingRows, SettingsCard } from "../SettingsPrimitives";

export function PrivacySection() {
  return (
    <SectionPage section="privacy">
      <SettingsCard
        title="Incognito"
        description="Incognito mode stops reading activity from being saved. Progress, history, session time and automatic tracker updates are all skipped while it is on."
      >
        <SettingRows section="privacy" />
      </SettingsCard>
    </SectionPage>
  );
}
