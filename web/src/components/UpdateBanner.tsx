import { useState, useSyncExternalStore } from "react";
import { getInstallState, subscribeInstallPrompt } from "../app/installPrompt";
import { applyPwaUpdate, getPwaUpdateState, subscribePwaUpdate } from "../app/pwaUpdate";
import { Notice } from "./Notice";

// Floating prompt shown on every page while a new service worker waits.
// Dismissal lasts until the next update event; the banner stays hidden in
// standalone display mode, where closing and reopening the installed app
// applies the update without prompting. Sits below the modal layer (z-50)
// and clears the mobile bottom nav.
export function UpdateBanner() {
  const { needRefresh, refreshEvents } = useSyncExternalStore(
    subscribePwaUpdate,
    getPwaUpdateState,
  );
  const { standalone } = useSyncExternalStore(subscribeInstallPrompt, getInstallState);
  const [dismissedAt, setDismissedAt] = useState(0);
  if (!needRefresh || standalone || dismissedAt === refreshEvents) return null;
  return (
    <div className="fixed inset-x-4 bottom-20 z-40 flex justify-center sm:bottom-6">
      <Notice
        className="w-full max-w-md"
        message="A new version of MakiDoku is ready."
        actionLabel="Reload"
        onAction={() => void applyPwaUpdate()}
        onDismiss={() => setDismissedAt(refreshEvents)}
      />
    </div>
  );
}
