package engine

// PreferredCoverWidth is the host-wide cover rendition target in served
// pixels.
const PreferredCoverWidth = 512

// SelectCover resolves the cover URL to display for a title. Among variants
// that declare a width it prefers the largest width not exceeding target,
// then the smallest declared variant, then the first entry in source order,
// and finally the canonical locator. The canonical locator stays untouched
// when no variant qualifies.
func SelectCover(coverURL string, variants []CoverVariant, target int) string {
	var best, smallest *CoverVariant
	for i := range variants {
		variant := &variants[i]
		if variant.Width == nil {
			continue
		}
		if *variant.Width <= target && (best == nil || *variant.Width > *best.Width) {
			best = variant
		}
		if smallest == nil || *variant.Width < *smallest.Width {
			smallest = variant
		}
	}
	switch {
	case best != nil:
		return best.URL
	case smallest != nil:
		return smallest.URL
	case len(variants) > 0:
		return variants[0].URL
	default:
		return coverURL
	}
}
