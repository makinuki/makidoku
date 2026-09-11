package tachibackup

import "sort"

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
}

// TrackerReport describes one tracker present in a backup.
type TrackerReport struct {
	SyncID      int32  `json:"syncId"`
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
}

// Options controls how backup sources and titles are resolved.
type Options struct {
	// SourceMap overrides automatic matching, keyed by backup source id. An
	// entry mapped to an empty string defers that source explicitly.
	SourceMap map[int64]string `json:"sourceMap,omitempty"`
	// SkipUnmatched drops titles whose source could not be matched instead of
	// deferring them.
	SkipUnmatched bool `json:"skipUnmatched,omitempty"`
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
	backup  *Backup
	sources map[int64]*resolvedSource
}

// Build resolves every source in a backup against the installed sources and
// returns the resulting plan. No database access and no writes occur here.
func Build(backup *Backup, installed []SourceRef, options Options) *Plan {
	plan := &Plan{
		backup:  backup,
		sources: map[int64]*resolvedSource{},
	}
	if backup == nil {
		return plan
	}
	plan.Counts.Manga = len(backup.Manga)
	plan.Counts.Categories = len(backup.Categories)
	for index := range backup.Manga {
		manga := &backup.Manga[index]
		plan.Counts.Chapters += len(manga.Chapters)
		plan.Counts.History += len(manga.History)
		plan.Counts.Trackings += len(manga.Tracking)
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
	for index := range backup.Manga {
		manga := &backup.Manga[index]
		resolved, ok := plan.sources[manga.Source]
		if !ok {
			resolved = &resolvedSource{name: names[manga.Source]}
			plan.sources[manga.Source] = resolved
			order = append(order, manga.Source)
		}
		resolved.manga++
		if manga.URL != "" {
			seriesURLs[manga.Source] = append(seriesURLs[manga.Source], manga.URL)
		}
		if manga.ThumbnailURL != "" {
			seriesURLs[manga.Source] = append(seriesURLs[manga.Source], manga.ThumbnailURL)
		}
	}

	for _, sourceID := range order {
		resolved := plan.sources[sourceID]
		if override, present := options.SourceMap[sourceID]; present {
			if ref, found := findSource(installed, override); found {
				resolved.ref, resolved.match = ref, "manual"
			} else {
				resolved.deferred, resolved.match = true, "deferred"
			}
		} else if ref, match, ok := matchSource(resolved.name, seriesURLs[sourceID], installed); ok {
			resolved.ref, resolved.match = ref, match
		} else {
			resolved.deferred, resolved.match = true, "unmatched"
		}
		if resolved.deferred {
			resolved.skip = options.SkipUnmatched
			plan.UnmatchedTitles += resolved.manga
		}
		plan.Sources = append(plan.Sources, SourceReport{
			BackupSourceID:    sourceID,
			Name:              resolved.name,
			MangaCount:        resolved.manga,
			MatchedSourceID:   resolved.ref.ID,
			MatchedSourceName: resolved.ref.Name,
			Match:             resolved.match,
			Deferred:          resolved.deferred,
		})
	}

	trackers := map[int32]*TrackerReport{}
	trackerOrder := []int32{}
	for index := range backup.Manga {
		for _, tracking := range backup.Manga[index].Tracking {
			entry, ok := trackers[tracking.SyncID]
			if !ok {
				name, supported := TrackerTypeName(tracking.SyncID)
				entry = &TrackerReport{SyncID: tracking.SyncID, TrackerType: name, Supported: supported}
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

// TrackerTypeName resolves a numeric tracker identifier. The second result
// reports whether MakiDoku has a tracker for it.
func TrackerTypeName(syncID int32) (string, bool) {
	name, ok := trackerTypes[syncID]
	return name, ok
}

// trackerStatuses maps the numeric tracker status onto the label stored with a
// binding.
func trackerStatus(status int32) *string {
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
