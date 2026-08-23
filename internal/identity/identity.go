// Package identity creates and validates opaque MakiDoku identifiers.
package identity

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// New returns a UUIDv7 identifier. UUIDv7 keeps creation order in its first
// 48 bits while the remaining bits are random.
func New() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate identity: %w", err)
	}
	millis := time.Now().UnixMilli()
	for index := 0; index < 6; index++ {
		value[index] = byte(millis >> (40 - index*8))
	}
	value[6] = (value[6] & 0x0f) | 0x70
	value[8] = (value[8] & 0x3f) | 0x80
	var encoded [36]byte
	hex.Encode(encoded[0:8], value[0:4])
	encoded[8] = '-'
	hex.Encode(encoded[9:13], value[4:6])
	encoded[13] = '-'
	hex.Encode(encoded[14:18], value[6:8])
	encoded[18] = '-'
	hex.Encode(encoded[19:23], value[8:10])
	encoded[23] = '-'
	hex.Encode(encoded[24:36], value[10:16])
	return string(encoded[:]), nil
}

// IsValid reports whether value is a canonical UUIDv7 string.
func IsValid(value string) bool {
	if len(value) != 36 || strings.ToLower(value) != value {
		return false
	}
	for index, character := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if character != '-' {
				return false
			}
			continue
		}
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f')) {
			return false
		}
	}
	return value[14] == '7' && (value[19] == '8' || value[19] == '9' || value[19] == 'a' || value[19] == 'b')
}
