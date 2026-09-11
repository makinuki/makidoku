// Package chapterrecog derives a chapter number from a chapter title when a
// source does not declare one. The rules follow common manga-release naming:
// a number introduced by a "ch." marker wins over a bare number, volume,
// version, and season markers are ignored, and alphabetic suffixes map onto
// fractional parts ("12.a" is chapter 12.1, "12.extra" is chapter 12.99).
package chapterrecog

import (
	"regexp"
	"strconv"
	"strings"
)

// numberPattern matches a chapter number with an optional decimal part and an
// optional alphabetic suffix, for example "12", "12.5", "12.a", or "12.extra".
const numberPattern = `([0-9]+)(\.[0-9]+)?(\.?[a-z]+)?`

var (
	number = regexp.MustCompile(numberPattern)
	// basic matches the number that follows a "ch." marker. Lookbehind is not
	// available, so the marker is consumed as a prefix; it carries no capture
	// group, so the submatch indexes stay aligned with numberPattern.
	basic = regexp.MustCompile(`ch\. *` + numberPattern)
	// unwanted matches volume, version, and season markers that must be removed
	// before a number is selected from a title that contains several numbers.
	unwanted = regexp.MustCompile(`\b(?:v|ver|vol|version|volume|season|s)[^a-z]?[0-9]+`)
	// unwantedWhiteSpace matches the space that separates a number from an
	// extra, omake, or special suffix.
	unwantedWhiteSpace = regexp.MustCompile(`\s(extra|special|omake)`)
)

// Parse returns the chapter number for a chapter named chapterName in a series
// titled mangaTitle. A declared number is returned unchanged when it carries a
// usable value. The result is nil when no number can be derived.
func Parse(mangaTitle, chapterName string, declared *float64) *float64 {
	if declared != nil && (*declared == -2 || *declared > -1) {
		return declared
	}
	clean := strings.ToLower(chapterName)
	if title := strings.ToLower(strings.TrimSpace(mangaTitle)); title != "" {
		clean = strings.ReplaceAll(clean, title, "")
	}
	clean = strings.TrimSpace(clean)
	clean = strings.ReplaceAll(clean, ",", ".")
	clean = strings.ReplaceAll(clean, "-", ".")
	clean = unwantedWhiteSpace.ReplaceAllString(clean, "$1")

	matches := number.FindAllStringSubmatch(clean, -1)
	if len(matches) == 0 {
		return declared
	}
	if len(matches) > 1 {
		stripped := unwanted.ReplaceAllString(clean, "")
		if match := basic.FindStringSubmatch(stripped); match != nil {
			return floatPointer(numberFromMatch(match))
		}
		if match := number.FindStringSubmatch(stripped); match != nil {
			return floatPointer(numberFromMatch(match))
		}
	}
	return floatPointer(numberFromMatch(matches[0]))
}

// numberFromMatch reads a numberPattern submatch. Group 1 is the integer part,
// group 2 is an optional decimal part, and group 3 is an optional alphabetic
// suffix.
func numberFromMatch(match []string) float64 {
	initial, err := strconv.ParseFloat(match[1], 64)
	if err != nil {
		return 0
	}
	var decimal, alpha string
	if len(match) > 2 {
		decimal = match[2]
	}
	if len(match) > 3 {
		alpha = match[3]
	}
	return initial + suffix(decimal, alpha)
}

// suffix converts a decimal or alphabetic suffix into the fraction added to the
// integer part. A decimal suffix wins when present; otherwise named suffixes map
// to fixed fractions and a single letter maps to one tenth of its position.
func suffix(decimal, alpha string) float64 {
	if decimal != "" {
		value, err := strconv.ParseFloat(decimal, 64)
		if err != nil {
			return 0
		}
		return value
	}
	if alpha != "" {
		if strings.Contains(alpha, "extra") {
			return 0.99
		}
		if strings.Contains(alpha, "omake") {
			return 0.98
		}
		if strings.Contains(alpha, "special") {
			return 0.97
		}
		trimmed := strings.TrimLeft(alpha, ".")
		if len(trimmed) == 1 {
			return alphaPostFix(trimmed[0])
		}
	}
	return 0
}

// alphaPostFix maps a letter to its fractional value: "a" is 0.1 through "i"
// is 0.9. Anything past "i" has no fractional meaning and yields 0.
func alphaPostFix(alpha byte) float64 {
	offset := int(alpha) - int('a'-1)
	if offset >= 10 {
		return 0
	}
	return float64(offset) / 10
}

func floatPointer(value float64) *float64 { return &value }
