import { SectionPage, SettingRows, SettingsCard } from "../SettingsPrimitives";

export function MigrationSection() {
  return (
    <SectionPage section="migration">
      <SettingsCard
        title="When a title moves to another plugin"
        description="A migration keeps the library entry together with its categories, tracker links, notes, custom cover and reader settings. These options control the chapter data tied to the plugin being left behind."
      >
        <SettingRows section="migration" />
      </SettingsCard>
    </SectionPage>
  );
}
