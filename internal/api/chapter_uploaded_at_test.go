package api

import "testing"

// A source declares an upload time in milliseconds while the database stores
// seconds, so a millisecond value is folded at the boundary and a seconds
// value is left as it is.
func TestChapterUploadedAt(t *testing.T) {
	milliseconds := int64(1_700_000_000_000)
	seconds := int64(1_600_000_000)
	if got := chapterUploadedAt(&milliseconds); got == nil || *got != milliseconds/1000 {
		t.Fatalf("millisecond value folded to %v, want %d", got, milliseconds/1000)
	}
	if got := chapterUploadedAt(&seconds); got == nil || *got != seconds {
		t.Fatalf("seconds value changed to %v", got)
	}
	if got := chapterUploadedAt(nil); got != nil {
		t.Fatalf("absent value became %v", *got)
	}
}
