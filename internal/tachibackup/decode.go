package tachibackup

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"math"

	"google.golang.org/protobuf/encoding/protowire"
)

// ErrJSON reports a file that carries a JSON document. JSON is a different
// format and is not converted here.
var ErrJSON = errors.New("JSON backups are not supported")

// maxDecodedBytes caps the decompressed backup size.
const maxDecodedBytes = 512 << 20

// Decode reads a backup document. The payload is either a gzip stream or a raw
// protobuf message; a JSON document is rejected explicitly.
func Decode(data []byte) (*Backup, error) {
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		reader, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("read gzip backup: %w", err)
		}
		defer reader.Close()
		plain, err := io.ReadAll(io.LimitReader(reader, maxDecodedBytes+1))
		if err != nil {
			return nil, fmt.Errorf("read gzip backup: %w", err)
		}
		if len(plain) > maxDecodedBytes {
			return nil, errors.New("backup is too large")
		}
		data = plain
	} else if len(data) >= 2 && data[0] == 0x7b {
		switch data[1] {
		case 0x7d, 0x22, 0x0a:
			return nil, ErrJSON
		}
	}
	backup, err := decodeBackup(data)
	if err != nil {
		return nil, fmt.Errorf("decode backup: %w", err)
	}
	return backup, nil
}

func decodeBackup(data []byte) (*Backup, error) {
	out := &Backup{}
	err := forEachField(data, func(f field) error {
		switch f.num {
		case 1:
			raw, err := f.bytes()
			if err != nil {
				return err
			}
			manga, err := decodeManga(raw)
			if err != nil {
				return err
			}
			out.Manga = append(out.Manga, manga)
		case 2:
			raw, err := f.bytes()
			if err != nil {
				return err
			}
			category, err := decodeCategory(raw)
			if err != nil {
				return err
			}
			out.Categories = append(out.Categories, category)
		case 101:
			raw, err := f.bytes()
			if err != nil {
				return err
			}
			source, err := decodeSource(raw)
			if err != nil {
				return err
			}
			out.Sources = append(out.Sources, source)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(out.Manga) == 0 && len(out.Categories) == 0 && len(out.Sources) == 0 {
		return nil, errors.New("no backup records found")
	}
	return out, nil
}

func decodeManga(data []byte) (Manga, error) {
	// The writer omits values equal to a field's declared default; the
	// favourite flag defaults to true, so an absent field means "in library".
	out := Manga{Favorite: true}
	err := forEachField(data, func(f field) error {
		var err error
		switch f.num {
		case 1:
			out.Source, err = f.int64()
		case 2:
			out.URL, err = f.string()
		case 3:
			out.Title, err = f.string()
		case 4:
			out.Artist, err = f.string()
		case 5:
			out.Author, err = f.string()
		case 6:
			out.Description, err = f.string()
		case 7:
			var genre string
			if genre, err = f.string(); err == nil {
				out.Genre = append(out.Genre, genre)
			}
		case 8:
			out.Status, err = f.int32()
		case 9:
			out.ThumbnailURL, err = f.string()
		case 13:
			out.DateAdded, err = f.int64()
		case 14:
			out.Viewer, err = f.int32()
		case 16:
			var raw []byte
			if raw, err = f.bytes(); err != nil {
				return err
			}
			var chapter Chapter
			if chapter, err = decodeChapter(raw); err != nil {
				return err
			}
			out.Chapters = append(out.Chapters, chapter)
		case 17:
			var id int64
			if id, err = f.int64(); err == nil {
				out.Categories = append(out.Categories, id)
			}
		case 18:
			var raw []byte
			if raw, err = f.bytes(); err != nil {
				return err
			}
			var tracking Tracking
			if tracking, err = decodeTracking(raw); err != nil {
				return err
			}
			out.Tracking = append(out.Tracking, tracking)
		case 100:
			out.Favorite, err = f.boolean()
		case 101:
			out.ChapterFlags, err = f.int32()
		case 103:
			var value int32
			if value, err = f.int32(); err == nil {
				out.ViewerFlags = &value
			}
		case 104:
			var raw []byte
			if raw, err = f.bytes(); err != nil {
				return err
			}
			var history History
			if history, err = decodeHistory(raw); err != nil {
				return err
			}
			out.History = append(out.History, history)
		case 105:
			out.UpdateStrategy, err = f.int32()
		case 106:
			out.LastModifiedAt, err = f.int64()
		case 107:
			var value int64
			if value, err = f.int64(); err == nil {
				out.FavoriteModifiedAt = &value
			}
		case 108:
			var scanlator string
			if scanlator, err = f.string(); err == nil {
				out.ExcludedScanlators = append(out.ExcludedScanlators, scanlator)
			}
		case 109:
			out.Version, err = f.int64()
		case 110:
			out.Notes, err = f.string()
		case 111:
			out.Initialized, err = f.boolean()
		}
		return err
	})
	return out, err
}

func decodeChapter(data []byte) (Chapter, error) {
	var out Chapter
	err := forEachField(data, func(f field) error {
		var err error
		switch f.num {
		case 1:
			out.URL, err = f.string()
		case 2:
			out.Name, err = f.string()
		case 3:
			out.Scanlator, err = f.string()
		case 4:
			out.Read, err = f.boolean()
		case 5:
			out.Bookmark, err = f.boolean()
		case 6:
			out.LastPageRead, err = f.int64()
		case 7:
			out.DateFetch, err = f.int64()
		case 8:
			out.DateUpload, err = f.int64()
		case 9:
			out.ChapterNumber, err = f.float32()
		case 10:
			out.SourceOrder, err = f.int64()
		case 11:
			out.LastModifiedAt, err = f.int64()
		case 12:
			out.Version, err = f.int64()
		}
		return err
	})
	return out, err
}

func decodeHistory(data []byte) (History, error) {
	var out History
	err := forEachField(data, func(f field) error {
		var err error
		switch f.num {
		case 1:
			out.URL, err = f.string()
		case 2:
			out.LastRead, err = f.int64()
		case 3:
			out.ReadDuration, err = f.int64()
		}
		return err
	})
	return out, err
}

func decodeCategory(data []byte) (Category, error) {
	var out Category
	err := forEachField(data, func(f field) error {
		var err error
		switch f.num {
		case 1:
			out.Name, err = f.string()
		case 2:
			out.Order, err = f.int64()
		case 3:
			out.ID, err = f.int64()
		case 100:
			out.Flags, err = f.int64()
		}
		return err
	})
	return out, err
}

func decodeTracking(data []byte) (Tracking, error) {
	var out Tracking
	err := forEachField(data, func(f field) error {
		var err error
		switch f.num {
		case 1:
			out.SyncID, err = f.int32()
		case 2:
			out.LibraryID, err = f.int64()
		case 3:
			out.MediaIDInt, err = f.int32()
		case 4:
			out.TrackingURL, err = f.string()
		case 5:
			out.Title, err = f.string()
		case 6:
			out.LastChapterRead, err = f.float32()
		case 7:
			out.TotalChapters, err = f.int32()
		case 8:
			out.Score, err = f.float32()
		case 9:
			out.Status, err = f.int32()
		case 10:
			out.StartedReadingDate, err = f.int64()
		case 11:
			out.FinishedReadingDate, err = f.int64()
		case 12:
			out.Private, err = f.boolean()
		case 100:
			out.MediaID, err = f.int64()
		}
		return err
	})
	return out, err
}

func decodeSource(data []byte) (Source, error) {
	var out Source
	err := forEachField(data, func(f field) error {
		var err error
		switch f.num {
		case 1:
			out.Name, err = f.string()
		case 2:
			out.SourceID, err = f.int64()
		}
		return err
	})
	return out, err
}

// field is one decoded protobuf field: its number, wire type, and the raw
// value bytes that follow the tag.
type field struct {
	num protowire.Number
	typ protowire.Type
	raw []byte
}

// forEachField walks a message, handing each field to visit in order. A field
// the caller does not recognise is skipped by the walk itself.
func forEachField(data []byte, visit func(field) error) error {
	for len(data) > 0 {
		num, typ, n := protowire.ConsumeTag(data)
		if n < 0 {
			return protowire.ParseError(n)
		}
		data = data[n:]
		size := protowire.ConsumeFieldValue(num, typ, data)
		if size < 0 {
			return protowire.ParseError(size)
		}
		if err := visit(field{num: num, typ: typ, raw: data[:size]}); err != nil {
			return err
		}
		data = data[size:]
	}
	return nil
}

func (f field) varint() (uint64, error) {
	value, n := protowire.ConsumeVarint(f.raw)
	if n < 0 {
		return 0, protowire.ParseError(n)
	}
	return value, nil
}

func (f field) int32() (int32, error) {
	value, err := f.varint()
	return int32(value), err
}

func (f field) int64() (int64, error) {
	value, err := f.varint()
	return int64(value), err
}

func (f field) boolean() (bool, error) {
	value, err := f.varint()
	return value != 0, err
}

func (f field) float32() (float32, error) {
	bits, n := protowire.ConsumeFixed32(f.raw)
	if n < 0 {
		return 0, protowire.ParseError(n)
	}
	return math.Float32frombits(bits), nil
}

func (f field) bytes() ([]byte, error) {
	value, n := protowire.ConsumeBytes(f.raw)
	if n < 0 {
		return nil, protowire.ParseError(n)
	}
	return value, nil
}

func (f field) string() (string, error) {
	value, err := f.bytes()
	return string(value), err
}
