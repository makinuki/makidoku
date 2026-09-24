import { useEffect, useState } from "react";
import { Navigate, Route, Routes, useLocation } from "react-router-dom";
import { AppShell } from "./app/AppShell";
import { BrowsePage } from "./features/browse/BrowsePage";
import { DownloadsPage } from "./features/downloads/DownloadsPage";
import { DetailsPage } from "./features/manga/DetailsPage";
import { HistoryPage } from "./features/history/HistoryPage";
import { LibraryPage } from "./features/library/LibraryPage";
import { MorePage } from "./features/more/MorePage";
import { ReaderPage } from "./features/reader/ReaderPage";
import { SettingsIndexPage } from "./features/settings/SettingsIndexPage";
import { SettingsLayout } from "./features/settings/SettingsLayout";
import { AppearanceSection } from "./features/settings/sections/AppearanceSection";
import { LibrarySection } from "./features/settings/sections/LibrarySection";
import { ReaderSection } from "./features/settings/sections/ReaderSection";
import { DownloadsSection } from "./features/settings/sections/DownloadsSection";
import { TrackingSection } from "./features/settings/sections/TrackingSection";
import { BrowseSection } from "./features/settings/sections/BrowseSection";
import { DataSection } from "./features/settings/sections/DataSection";
import { PrivacySection } from "./features/settings/sections/PrivacySection";
import { AdvancedSection } from "./features/settings/sections/AdvancedSection";
import { UpdatesPage } from "./features/updates/UpdatesPage";
import { GlobalSearch } from "./features/search/GlobalSearch";
import { RecommendationsPage } from "./features/manga/RecommendationsPage";
import { StatsPage } from "./features/stats/StatsPage";
import { ErrorBoundary } from "./components/ErrorBoundary";
import { UpdateBanner } from "./components/UpdateBanner";

export default function App() {
  const [searchOpen, setSearchOpen] = useState(false);
  const routeLocation = useLocation();
  const settingsRoute =
    routeLocation.pathname === "/settings" || routeLocation.pathname.startsWith("/settings/");
  const searchMode = settingsRoute ? "settings" : "library";
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
    <AppShell onSearch={() => setSearchOpen(true)} searchMode={searchMode}>
      <ErrorBoundary key={routeLocation.pathname}>
        <Routes>
          <Route path="/" element={<LibraryPage />} />
          <Route path="/library" element={<LibraryPage />} />
          <Route path="/browse" element={<BrowsePage />} />
          <Route path="/manga/:mangaId" element={<DetailsPage />} />
          <Route path="/manga/:mangaId/recommendations" element={<RecommendationsPage />} />
          <Route path="/downloads" element={<DownloadsPage />} />
          <Route path="/history" element={<HistoryPage />} />
          <Route path="/more" element={<MorePage />} />
          <Route path="/updates" element={<UpdatesPage />} />
          <Route path="/stats" element={<StatsPage />} />
          <Route path="/settings" element={<SettingsLayout />}>
            <Route index element={<SettingsIndexPage />} />
            <Route path="appearance" element={<AppearanceSection />} />
            <Route path="library" element={<LibrarySection />} />
            <Route path="reader" element={<ReaderSection />} />
            <Route path="downloads" element={<DownloadsSection />} />
            <Route path="tracking" element={<TrackingSection />} />
            <Route path="browse" element={<BrowseSection />} />
            <Route path="data" element={<DataSection />} />
            <Route path="privacy" element={<PrivacySection />} />
            <Route path="advanced" element={<AdvancedSection />} />
            <Route path="*" element={<Navigate to="/settings" replace />} />
          </Route>
          <Route path="/reader/:mangaId/:chapterId" element={<ReaderPage />} />
          <Route path="/reader" element={<ReaderPage />} />
        </Routes>
        {searchOpen && <GlobalSearch mode={searchMode} onClose={() => setSearchOpen(false)} />}
      </ErrorBoundary>
      <UpdateBanner />
    </AppShell>
  );
}
