import { create } from "zustand";
import { api } from "./api";
import type { Category, DownloadSnapshot, LibraryManga, Source } from "./types";

type AppState = {
  library: LibraryManga[];
  categories: Category[];
  sources: Source[];
  downloads?: DownloadSnapshot;
  loading: boolean;
  error?: string;
  loadLibrary: (query?: string, category?: number) => Promise<void>;
  loadCategories: () => Promise<void>;
  loadSources: () => Promise<void>;
  loadDownloads: () => Promise<void>;
};
export const useAppStore = create<AppState>((set) => ({
  library: [],
  categories: [],
  sources: [],
  loading: false,
  loadLibrary: async (query, category) => {
    set({ loading: true, error: undefined });
    try {
      set({ library: await api.library(query, category), loading: false });
    } catch (e) {
      set({ loading: false, error: e instanceof Error ? e.message : "Unable to load library" });
    }
  },
  loadCategories: async () => {
    try {
      set({ categories: await api.categories() });
    } catch (e) {
      set({ error: e instanceof Error ? e.message : "Unable to load categories" });
    }
  },
  loadSources: async () => {
    try {
      set({ sources: await api.sources() });
    } catch (e) {
      set({ error: e instanceof Error ? e.message : "Unable to load sources" });
    }
  },
  loadDownloads: async () => {
    try {
      set({ downloads: await api.downloads() });
    } catch (e) {
      set({ error: e instanceof Error ? e.message : "Unable to load downloads" });
    }
  },
}));
