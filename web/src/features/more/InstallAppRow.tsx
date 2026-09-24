import { useState, useSyncExternalStore } from "react";
import { ChevronRight, MonitorSmartphone, Share } from "lucide-react";
import { Modal } from "../../components/Modal";
import { getInstallState, promptInstall, subscribeInstallPrompt } from "../../app/installPrompt";

// "Install app" entry point for the More page. Hidden once the app runs
// installed, and hidden entirely on platforms that can neither show the
// captured Chromium prompt nor the iOS manual flow. Renders its own section
// so the page never shows an empty bordered box when the row is hidden.
export function InstallAppRow() {
  const install = useSyncExternalStore(subscribeInstallPrompt, getInstallState);
  const [showInstructions, setShowInstructions] = useState(false);
  if (install.standalone) return null;
  if (!install.canPrompt && !install.isIos) return null;
  const onClick = () => {
    if (install.canPrompt) {
      void promptInstall();
    } else {
      setShowInstructions(true);
    }
  };
  return (
    <section className="overflow-hidden rounded-xl border border-zinc-800 bg-zinc-900/40">
      <button
        type="button"
        onClick={onClick}
        className="flex min-h-11 w-full items-center gap-4 px-4 py-3.5 text-left hover:bg-zinc-800/50 active:bg-zinc-800/50"
      >
        <MonitorSmartphone size={18} className="shrink-0 text-amber-400" />
        <span className="min-w-0 flex-1">
          <span className="block text-sm font-medium">Install app</span>
          <span className="block truncate text-xs text-zinc-500">
            Add MakiDoku to your home screen
          </span>
        </span>
        <ChevronRight size={16} className="shrink-0 text-zinc-600" />
      </button>
      {showInstructions && (
        <Modal title="Install MakiDoku" variant="sheet" onClose={() => setShowInstructions(false)}>
          <p className="mb-4 text-sm text-zinc-400">
            iOS installs web apps from Safari only. If you are reading this in another browser, open
            this page in Safari first.
          </p>
          <ol className="list-decimal space-y-3 pl-5 text-sm text-zinc-300">
            <li>
              Tap the Share button{" "}
              <Share size={14} aria-label="Share" className="inline-block align-text-bottom" /> in
              the Safari toolbar.
            </li>
            <li>Scroll down and tap Add to Home Screen.</li>
            <li>Tap Add in the top-right corner.</li>
          </ol>
        </Modal>
      )}
    </section>
  );
}
