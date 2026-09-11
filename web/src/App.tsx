import { useEffect, useState } from "react";
import { Route, Routes, useLocation } from "react-router-dom";
import { AppShell } from "./app/AppShell";
import { BrowsePage } from "./features/browse/BrowsePage";
import { DownloadsPage } from "./features/downloads/DownloadsPage";
import { DetailsPage } from "./features/manga/DetailsPage";
import { HistoryPage } from "./features/history/HistoryPage";
import { LibraryPage } from "./features/library/LibraryPage";
import { ReaderPage } from "./features/reader/ReaderPage";
import { SettingsPage } from "./features/settings/SettingsPage";
import { UpdatesPage } from "./features/updates/UpdatesPage";
import { GlobalSearch } from "./features/search/GlobalSearch";
import { RecommendationsPage } from "./features/manga/RecommendationsPage";
import { ErrorBoundary } from "./components/ErrorBoundary";

export default function App() {
  const [searchOpen, setSearchOpen] = useState(false);
  const routeLocation = useLocation();
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
      <ErrorBoundary key={routeLocation.pathname}>
        <Routes>
          <Route path="/" element={<LibraryPage />} />
          <Route path="/library" element={<LibraryPage />} />
          <Route path="/browse" element={<BrowsePage />} />
          <Route path="/manga/:mangaId" element={<DetailsPage />} />
          <Route path="/manga/:mangaId/recommendations" element={<RecommendationsPage />} />
          <Route path="/downloads" element={<DownloadsPage />} />
          <Route path="/history" element={<HistoryPage />} />
          <Route path="/updates" element={<UpdatesPage />} />
          <Route path="/settings" element={<SettingsPage />} />
          <Route path="/reader/:mangaId/:chapterId" element={<ReaderPage />} />
          <Route path="/reader" element={<ReaderPage />} />
        </Routes>
        {searchOpen && <GlobalSearch onClose={() => setSearchOpen(false)} />}
      </ErrorBoundary>
    </AppShell>
  );
}
