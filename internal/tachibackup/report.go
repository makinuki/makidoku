package tachibackup

import (
	"sort"
	"strings"
)

// sampleTitleLimit bounds how many example titles a source report carries.
const sampleTitleLimit = 3

// Counts summarises the records a backup carries.
type Counts struct {
	Manga      int `json:"manga"`
	Chapters   int `json:"chapters"`
	Categories int `json:"categories"`
	History    int `json:"history"`
	Trackings  int `json:"trackings"`
}

// SourceReport describes how one backup source maps onto an installed source.
// A deferred source has no installed match: its titles are imported against a
// placeholder source so their state survives, unless the caller asks to skip
// unmatched titles.
type SourceReport struct {
	BackupSourceID    int64  `json:"backupSourceId"`
	Name              string `json:"name"`
	MangaCount        int    `json:"mangaCount"`
	MatchedSourceID   string `json:"matchedSourceId,omitempty"`
	MatchedSourceName string `json:"matchedSourceName,omitempty"`
	Match             string `json:"match,omitempty"`
	Deferred          bool   `json:"deferred"`
	// DetectedSite and SuggestedName identify an unmatched source from the
	// locators it carries, so a user can confirm a mapping without having to
	// recognise a numeric identifier.
	DetectedSite  string `json:"detectedSite,omitempty"`
	SuggestedName string `json:"suggestedName,omitempty"`
	// SampleTitles and SampleURL show what the source holds, which is what a
	// user needs in order to decide how to map it.
	SampleTitles []string `json:"sampleTitles,omitempty"`
	SampleURL    string   `json:"sampleURL,omitempty"`
}

// TrackerReport describes one tracker present in a backup.
type TrackerReport struct {
	SyncID      int32  `json:"syncId"`
	Name        string `json:"name,omitempty"`
	TrackerType string `json:"trackerType,omitempty"`
	Supported   bool   `json:"supported"`
	MangaCount  int    `json:"mangaCount"`
}

// Report is the dry-run result of reading a backup against the installed
// sources. Building a report performs no writes.
type Report struct {
	Counts               Counts          `json:"counts"`
	Sources              []SourceReport  `json:"sources"`
	Trackers             []TrackerReport `json:"trackers"`
	UnmatchedTitles      int             `json:"unmatchedTitles"`
	UnsupportedTrackings int             `json:"unsupportedTrackings"`
	// OutOfLibraryTitles counts titles the backup records outside the library
	// which still carry chapters, read state or history.
	OutOfLibraryTitles int `json:"outOfLibraryTitles"`
	// Preferences counts records the importer never persists. They are reported
	// so the user knows what the file carried.
	Preferences       int `json:"preferences"`
	SourcePreferences int `json:"sourcePreferences"`
	ExtensionStores   int `json:"extensionStores"`
}

// Options controls how backup sources and titles are resolved.
type Options struct {
	// SourceMap overrides automatic matching, keyed by backup source id. An
	// entry mapped to an empty string defers that source explicitly.
	SourceMap map[int64]string `json:"sourceMap,omitempty"`
	// SkipUnmatched drops titles whose source could not be matched instead of
	// deferring them.
	SkipUnmatched bool `json:"skipUnmatched,omitempty"`
	// SkipOutOfLibrary drops titles the backup records outside the library.
	// They are imported by default because they still carry read state.
	SkipOutOfLibrary bool `json:"skipOutOfLibrary,omitempty"`
}

// resolvedSource is the plan for one backup source.
type resolvedSource struct {
	name     string
	ref      SourceRef
	match    string
	manga    int
	deferred bool
	skip     bool
}

// Plan is a report together with the per-source decisions an import applies.
type Plan struct {
	Report
	backup           *Backup
	sources          map[int64]*resolvedSource
	skipOutOfLibrary bool
}

// Build resolves every source in a backup against the installed sources and
// returns the resulting plan. No database access and no writes occur here.
func Build(backup *Backup, installed []SourceRef, options Options) *Plan {
	plan := &Plan{
		backup:           backup,
		sources:          map[int64]*resolvedSource{},
		skipOutOfLibrary: options.SkipOutOfLibrary,
	}
	if backup == nil {
		return plan
	}
	plan.Counts.Manga = len(backup.Manga)
	plan.Counts.Categories = len(backup.Categories)
	plan.Preferences = backup.Preferences
	plan.SourcePreferences = backup.SourcePreferences
	plan.ExtensionStores = backup.ExtensionStores
	for index := range backup.Manga {
		manga := &backup.Manga[index]
		plan.Counts.Chapters += len(manga.Chapters)
		plan.Counts.History += len(manga.History)
		plan.Counts.Trackings += len(manga.Tracking)
		if !manga.Favorite {
			plan.OutOfLibraryTitles++
		}
	}

	names := map[int64]string{}
	order := []int64{}
	for _, source := range backup.Sources {
		if _, seen := names[source.SourceID]; !seen {
			order = append(order, source.SourceID)
			plan.sources[source.SourceID] = &resolvedSource{name: source.Name}
		}
		names[source.SourceID] = source.Name
	}
	seriesURLs := map[int64][]string{}
	coverURLs := map[int64][]string{}
	sampleTitles := map[int64][]string{}
	for index := range backup.Manga {
		manga := &backup.Manga[index]
		resolved, ok := plan.sources[manga.Source]
		if !ok {
			resolved = &resolvedSource{name: names[manga.Source]}
			plan.sources[manga.Source] = resolved
			order = append(order, manga.Source)
		}
		resolved.manga++
		if len(sampleTitles[manga.Source]) < sampleTitleLimit {
			if title := strings.TrimSpace(manga.Title); title != "" {
				sampleTitles[manga.Source] = append(sampleTitles[manga.Source], title)
			}
		}
		if manga.URL != "" {
			seriesURLs[manga.Source] = append(seriesURLs[manga.Source], manga.URL)
		}
		if manga.ThumbnailURL != "" {
			coverURLs[manga.Source] = append(coverURLs[manga.Source], manga.ThumbnailURL)
		}
	}

	for _, sourceID := range order {
		resolved := plan.sources[sourceID]
		probes := make([]string, 0, len(seriesURLs[sourceID])+len(coverURLs[sourceID]))
		probes = append(probes, seriesURLs[sourceID]...)
		probes = append(probes, coverURLs[sourceID]...)
		if override, present := options.SourceMap[sourceID]; present {
			if ref, found := findSource(installed, override); found {
				resolved.ref, resolved.match = ref, "manual"
			} else {
				resolved.deferred, resolved.match = true, "deferred"
			}
		} else if ref, match, ok := matchSource(resolved.name, probes, installed); ok {
			resolved.ref, resolved.match = ref, match
		} else {
			resolved.deferred, resolved.match = true, "unmatched"
		}
		if resolved.deferred {
			resolved.skip = options.SkipUnmatched
			plan.UnmatchedTitles += resolved.manga
		}
		report := SourceReport{
			BackupSourceID:    sourceID,
			Name:              resolved.name,
			MangaCount:        resolved.manga,
			MatchedSourceID:   resolved.ref.ID,
			MatchedSourceName: resolved.ref.Name,
			Match:             resolved.match,
			Deferred:          resolved.deferred,
			SampleTitles:      sampleTitles[sourceID],
		}
		if len(seriesURLs[sourceID]) > 0 {
			report.SampleURL = seriesURLs[sourceID][0]
		}
		if resolved.deferred {
			detection := detectSite(probes)
			report.DetectedSite = detection.Host
			report.SuggestedName = detection.Name
			if strings.TrimSpace(report.Name) == "" {
				report.Name = detection.Name
			}
		}
		plan.Sources = append(plan.Sources, report)
	}

	trackers := map[int32]*TrackerReport{}
	trackerOrder := []int32{}
	for index := range backup.Manga {
		for _, tracking := range backup.Manga[index].Tracking {
			entry, ok := trackers[tracking.SyncID]
			if !ok {
				name, supported := TrackerTypeName(tracking.SyncID)
				entry = &TrackerReport{SyncID: tracking.SyncID, Name: TrackerDisplayName(tracking.SyncID), TrackerType: name, Supported: supported}
				trackers[tracking.SyncID] = entry
				trackerOrder = append(trackerOrder, tracking.SyncID)
			}
			entry.MangaCount++
			if !entry.Supported {
				plan.UnsupportedTrackings++
			}
		}
	}
	for _, syncID := range trackerOrder {
		plan.Trackers = append(plan.Trackers, *trackers[syncID])
	}
	return plan
}

// CountsByTracker returns the number of bindings the plan would write.
func (p *Plan) CountsByTracker() int {
	total := 0
	for _, tracker := range p.Trackers {
		if tracker.Supported {
			total += tracker.MangaCount
		}
	}
	return total
}

// SourcesByID exposes the resolved sources for callers that need to render the
// mapping back to a client.
func (p *Plan) SourcesByID() map[int64]SourceReport {
	out := make(map[int64]SourceReport, len(p.Sources))
	for _, source := range p.Sources {
		out[source.BackupSourceID] = source
	}
	return out
}

// DeferredSourceIDs lists the backup source ids that have no installed match.
func (p *Plan) DeferredSourceIDs() []int64 {
	ids := []int64{}
	for id, resolved := range p.sources {
		if resolved.deferred {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func findSource(installed []SourceRef, reference string) (SourceRef, bool) {
	reference = normalizeName(reference)
	for _, candidate := range installed {
		if normalizeName(candidate.ID) == reference || normalizeName(candidate.PluginKey) == reference {
			return candidate, true
		}
	}
	return SourceRef{}, false
}

// trackerTypes maps the numeric tracker identifiers used by the source format
// onto the tracker names MakiDoku stores.
var trackerTypes = map[int32]string{
	1:  "myanimelist",
	2:  "anilist",
	3:  "kitsu",
	7:  "mangaupdates",
	11: "mangabaka",
}

// trackerDisplayNames names every identifier the backup format defines,
// including the trackers MakiDoku does not implement, so a report can say
// which tracker a skipped binding belonged to.
var trackerDisplayNames = map[int32]string{
	1:  "MyAnimeList",
	2:  "AniList",
	3:  "Kitsu",
	4:  "Shikimori",
	5:  "Bangumi",
	6:  "Komga",
	7:  "MangaUpdates",
	8:  "Kavita",
	9:  "Suwayomi",
	11: "MangaBaka",
	60: "MdList",
}

// TrackerTypeName resolves a numeric tracker identifier. The second result
// reports whether MakiDoku has a tracker for it.
func TrackerTypeName(syncID int32) (string, bool) {
	name, ok := trackerTypes[syncID]
	return name, ok
}

// TrackerDisplayName returns a human-readable name for a tracker identifier,
// falling back to the numeric value when the format does not define it.
func TrackerDisplayName(syncID int32) string {
	if name, ok := trackerDisplayNames[syncID]; ok {
		return name
	}
	return ""
}

// trackerStatus maps the numeric tracker status onto the label stored with a
// binding.
//
// The writing application omits a status equal to its declared default, and
// the default is zero for every tracker. Zero names a real status only where
// the tracker's own status space starts there; elsewhere an absent status
// stays unknown rather than being guessed.
func trackerStatus(trackerType string, status int32) *string {
	if status == 0 {
		if trackerType == "mangaupdates" {
			label := "reading"
			return &label
		}
		return nil
	}
	labels := map[int32]string{
		1: "reading",
		2: "completed",
		3: "on_hold",
		4: "dropped",
		5: "plan_to_read",
		6: "repeating",
	}
	if label, ok := labels[status]; ok {
		return &label
	}
	return nil
}

// mangaStatuses maps the numeric publication status onto the label MakiDoku
// stores.
func mangaStatus(status int32) string {
	switch status {
	case 1:
		return "Ongoing"
	case 2, 4:
		return "Completed"
	case 5:
		return "Cancelled"
	case 6:
		return "Hiatus"
	default:
		return "Unknown"
	}
}
