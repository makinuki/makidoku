import { SectionPage, SettingRows, SettingsCard } from "../SettingsPrimitives";

export function AdvancedSection() {
  return (
    <SectionPage section="advanced">
      <SettingsCard title="Diagnostics" description="Logging and cached image retention.">
        <SettingRows section="advanced" />
      </SettingsCard>
    </SectionPage>
  );
}
