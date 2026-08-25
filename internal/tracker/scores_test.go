package tracker

import "testing"

func TestScoreConversionTable(t *testing.T) {
	readCases := []struct {
		provider string
		native   float64
		metadata map[string]string
		want     float64
	}{
		{"anilist", 85, map[string]string{"score_format": "POINT_100"}, 8.5},
		{"anilist", 8, map[string]string{"score_format": "POINT_10"}, 8},
		{"anilist", 8.5, map[string]string{"score_format": "POINT_10_DECIMAL"}, 8.5},
		{"anilist", 4, map[string]string{"score_format": "POINT_5"}, 8},
		{"anilist", 2, map[string]string{"score_format": "POINT_3"}, 20.0 / 3},
		{"anilist", 7, nil, 7},
		{"kitsu", 16, nil, 8},
		{"kitsu", 8, nil, 8},
		{"mangabaka", 80, nil, 8},
		{"myanimelist", 8, nil, 8},
		{"mangaupdates", 7.5, nil, 7.5},
	}
	for _, tc := range readCases {
		if got := readScore(tc.provider, tc.native, tc.metadata); got != tc.want {
			t.Errorf("readScore(%s, %v, %v) = %v, want %v", tc.provider, tc.native, tc.metadata, got, tc.want)
		}
	}

	writeCases := []struct {
		provider string
		score    float64
		metadata map[string]string
		want     float64
	}{
		{"anilist", 8.5, map[string]string{"score_format": "POINT_100"}, 85},
		{"anilist", 8.4, map[string]string{"score_format": "POINT_10"}, 8},
		{"anilist", 8.5, map[string]string{"score_format": "POINT_10_DECIMAL"}, 8.5},
		{"anilist", 8.5, map[string]string{"score_format": "POINT_5"}, 4},
		{"anilist", 9.5, map[string]string{"score_format": "POINT_5"}, 5},
		{"anilist", 6, map[string]string{"score_format": "POINT_5"}, 3},
		{"anilist", 2, map[string]string{"score_format": "POINT_3"}, 1},
		{"anilist", 7, map[string]string{"score_format": "POINT_3"}, 3},
		{"anilist", 0, map[string]string{"score_format": "POINT_5"}, 0},
		{"kitsu", 8, nil, 16},
		{"kitsu", 0.5, nil, 2},
		{"kitsu", 0, nil, 0},
		{"kitsu", 10, nil, 20},
		{"mangabaka", 8, nil, 80},
		{"mangabaka", 8.24, nil, 82},
		{"myanimelist", 7.5, nil, 8},
		{"myanimelist", 7.4, nil, 7},
		{"mangaupdates", 7.5, nil, 7.5},
	}
	for _, tc := range writeCases {
		got, err := writeScore(tc.provider, tc.score, tc.metadata)
		if err != nil {
			t.Errorf("writeScore(%s, %v): %v", tc.provider, tc.score, err)
			continue
		}
		if got != tc.want {
			t.Errorf("writeScore(%s, %v, %v) = %v, want %v", tc.provider, tc.score, tc.metadata, got, tc.want)
		}
	}

	if _, err := writeScore("unknown", 5, nil); err == nil {
		t.Error("writeScore(unknown) should fail")
	}
}
