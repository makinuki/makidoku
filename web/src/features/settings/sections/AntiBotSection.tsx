import { SectionPage, SettingRows, SettingsCard } from "../SettingsPrimitives";

export function AntiBotSection() {
  return (
    <SectionPage section="anti_bot">
      <SettingsCard
        title="Browser check"
        description="When a site checks for a browser, a window can open by itself and answer the check, or it can wait behind a button on the browse grid. The automatic path opens at most three windows per site per run, so a site that never clears stops interrupting you; the button keeps working and is never counted against that allowance."
      >
        <SettingRows section="anti_bot" />
      </SettingsCard>
    </SectionPage>
  );
}