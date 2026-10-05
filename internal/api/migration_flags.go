package api

// migrationFlag selects what a migration carries over. It is a bitmask, and
// the values behind its bits are user preferences stored in the settings
// table.
//
// A migration keeps the library entry's Makidoku id and re-points it at the
// replacement source, so category membership, tracker bindings, notes, custom
// covers and reader overrides stay on the entry by construction. The flags
// cover the two pieces that are keyed to the retired chapters instead.
type migrationFlag int

const (
	// migrationFlagChapter carries read state and bookmarks onto the
	// replacement, matched by chapter number.
	migrationFlagChapter migrationFlag = 1 << iota
	// migrationFlagRemoveDownload deletes the downloaded files of the retired
	// chapters from disk.
	migrationFlagRemoveDownload
)

// migrationFlagDefault is the behaviour of a migration that has no stored
// preference.
const migrationFlagDefault = migrationFlagChapter | migrationFlagRemoveDownload

func (f migrationFlag) has(flag migrationFlag) bool { return f&flag != 0 }

// valid reports whether the mask only contains flags this build understands.
func (f migrationFlag) valid() bool { return f >= 0 && f&^migrationFlagDefault == 0 }

// migrationFlags resolves the stored preferences. A missing settings service
// or an unreadable preference falls back to the default behaviour.
func (s *Server) migrationFlags() migrationFlag {
	if s.settings == nil {
		return migrationFlagDefault
	}
	var flags migrationFlag
	if carry, err := s.settings.Bool("migration.carry_read_state"); err == nil && carry {
		flags |= migrationFlagChapter
	}
	if remove, err := s.settings.Bool("migration.remove_downloads"); err == nil && remove {
		flags |= migrationFlagRemoveDownload
	}
	return flags
}
