import { SectionPage, SettingRows, SettingsCard } from "../SettingsPrimitives";

export function ReaderSection() {
  return (
    <SectionPage section="reader">
      <SettingsCard title="Defaults" description="Applied whenever a chapter opens.">
        <SettingRows section="reader" />
      </SettingsCard>
    </SectionPage>
  );
}
