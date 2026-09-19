import { SectionPage, SettingRows, SettingsCard } from "../SettingsPrimitives";

export function ReaderSection() {
  return (
    <SectionPage section="reader">
      <SettingsCard title="Viewer" description="Applied whenever a chapter opens.">
        <SettingRows
          section="reader"
          keys={["reader.default_mode", "reader.direction", "reader.fit"]}
        />
      </SettingsCard>
      <SettingsCard title="Navigation" description="Tap and click zones for paged modes.">
        <SettingRows section="reader" keys={["reader.navigation"]} />
      </SettingsCard>
      <SettingsCard title="Display" description="Webtoon spacing, theme, and image filters.">
        <SettingRows
          section="reader"
          keys={[
            "reader.webtoon_gap",
            "reader.theme",
            "reader.brightness",
            "reader.grayscale",
            "reader.invert",
          ]}
        />
      </SettingsCard>
    </SectionPage>
  );
}
