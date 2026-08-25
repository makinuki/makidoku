package tracker

import (
	"fmt"
	"math"
)

// Canonical tracker scores are floats from 0 to 10. Providers expose
// different scales, and some honor the account's own scoring configuration,
// which is captured in the credential metadata at connect time:
//
//	anilist      score_format   POINT_100, POINT_10, POINT_10_DECIMAL, POINT_5, POINT_3
//	kitsu        rating_system  informational; the wire scale is always 2 to 20
//	mangabaka    rating_steps   informational; the wire scale is always 0 to 100
//	myanimelist  fixed 0 to 10 integers
//	mangaupdates fixed 0 to 10 floats

// readScore converts a provider-native rating to the canonical 0 to 10 scale.
func readScore(provider string, native float64, metadata map[string]string) float64 {
	switch provider {
	case "anilist":
		switch metadata["score_format"] {
		case "POINT_100":
			return native / 10
		case "POINT_5":
			return native * 2
		case "POINT_3":
			return native * 10 / 3
		default:
			return native
		}
	case "kitsu":
		// Advanced accounts rate on 20 points, everyone else on 10; the wire
		// value itself reveals the scale.
		if native > 10 {
			return native / 2
		}
		return native
	case "mangabaka":
		return native / 10
	default:
		return native
	}
}

// writeScore converts a canonical 0 to 10 score to the provider's wire scale.
// The result is meant to be serialized as-is: AniList receives its score as a
// string regardless of format, so fractional and symbolic values are fine.
func writeScore(provider string, score float64, metadata map[string]string) (float64, error) {
	switch provider {
	case "anilist":
		switch metadata["score_format"] {
		case "POINT_100":
			return roundClamp(score*10, 0, 100), nil
		case "POINT_5":
			switch {
			case score <= 0:
				return 0, nil
			case score < 3:
				return 1, nil
			case score < 5:
				return 2, nil
			case score < 7:
				return 3, nil
			case score < 9:
				return 4, nil
			default:
				return 5, nil
			}
		case "POINT_3":
			switch {
			case score <= 0:
				return 0, nil
			case score <= 3.5:
				return 1, nil
			case score <= 6:
				return 2, nil
			default:
				return 3, nil
			}
		case "POINT_10_DECIMAL":
			return clamp(score, 0, 10), nil
		default:
			return roundClamp(score, 0, 10), nil
		}
	case "kitsu":
		if score <= 0 {
			return 0, nil
		}
		return roundClamp(score*2, 2, 20), nil
	case "mangabaka":
		return roundClamp(score*10, 0, 100), nil
	case "myanimelist":
		return roundClamp(score, 0, 10), nil
	case "mangaupdates":
		return clamp(score, 0, 10), nil
	default:
		return 0, fmt.Errorf("unknown provider %s", provider)
	}
}

func clamp(value, min, max float64) float64 {
	return math.Min(math.Max(value, min), max)
}

func roundClamp(value, min, max float64) float64 {
	return clamp(math.Round(value), min, max)
}
