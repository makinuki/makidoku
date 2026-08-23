import { useEffect, useState } from "react";
import { Route, Routes } from "react-router-dom";
import { AppShell } from "./app/AppShell";
import { BrowsePage } from "./features/browse/BrowsePage";
import { DownloadsPage } from "./features/downloads/DownloadsPage";
import { DetailsPage } from "./features/manga/DetailsPage";
import { HistoryPage } from "./features/history/HistoryPage";
import { LibraryPage } from "./features/library/LibraryPage";
import { ReaderPage } from "./features/reader/ReaderPage";
import { SettingsPage } from "./features/settings/SettingsPage";
import { GlobalSearch } from "./features/search/GlobalSearch";

export default function App() {
  const [searchOpen, setSearchOpen] = useState(false);
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        setSearchOpen(true);
      }
      if (event.key === "Escape") setSearchOpen(false);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);
  return (
    <AppShell onSearch={() => setSearchOpen(true)}>
      <Routes>
        <Route path="/" element={<LibraryPage />} />
        <Route path="/library" element={<LibraryPage />} />
        <Route path="/browse" element={<BrowsePage />} />
        <Route path="/manga/:mangaId" element={<DetailsPage />} />
        <Route path="/downloads" element={<DownloadsPage />} />
        <Route path="/history" element={<HistoryPage />} />
        <Route path="/settings" element={<SettingsPage />} />
        <Route path="/reader/:mangaId/:chapterId" element={<ReaderPage />} />
        <Route path="/reader" element={<ReaderPage />} />
      </Routes>
      {searchOpen && <GlobalSearch onClose={() => setSearchOpen(false)} />}
    </AppShell>
  );
}
