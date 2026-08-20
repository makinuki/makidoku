package engine

import (
	"errors"
	"fmt"
	"net/http"
)

// ErrorCode is one of the standardized codes carried across the host
// boundary. No other values may reach a plugin or the REST API.
type ErrorCode string

const (
	CodeCloudflareBlocked   ErrorCode = "CLOUDFLARE_BLOCKED"
	CodeRateLimited         ErrorCode = "RATE_LIMITED"
	CodeNetworkTimeout      ErrorCode = "NETWORK_TIMEOUT"
	CodeSessionRequired     ErrorCode = "SESSION_REQUIRED"
	CodeAuthExpired         ErrorCode = "AUTH_EXPIRED"
	CodeNotFound            ErrorCode = "NOT_FOUND"
	CodeSourceOffline       ErrorCode = "SOURCE_OFFLINE"
	CodeParsingError        ErrorCode = "PARSING_ERROR"
	CodeUnsupportedMedia    ErrorCode = "UNSUPPORTED_MEDIA"
	CodeMemoryLimitExceeded ErrorCode = "MEMORY_LIMIT_EXCEEDED"
	CodeUnscrambleFailed    ErrorCode = "UNSCRAMBLE_FAILED"
)

var knownCodes = map[ErrorCode]struct{}{
	CodeCloudflareBlocked:   {},
	CodeRateLimited:         {},
	CodeNetworkTimeout:      {},
	CodeSessionRequired:     {},
	CodeAuthExpired:         {},
	CodeNotFound:            {},
	CodeSourceOffline:       {},
	CodeParsingError:        {},
	CodeUnsupportedMedia:    {},
	CodeMemoryLimitExceeded: {},
	CodeUnscrambleFailed:    {},
}

// KnownCode reports whether code is one of the standardized codes. A code
// reported by a plugin that fails this check is normalized to PARSING_ERROR
// rather than forwarded.
func KnownCode(code ErrorCode) bool {
	_, ok := knownCodes[code]
	return ok
}

// Error carries a standardized error code across the host boundary.
type Error struct {
	Code    ErrorCode
	Message string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Message) }

// CodedError builds an Error, normalizing unrecognized codes.
func CodedError(code ErrorCode, format string, args ...any) *Error {
	if !KnownCode(code) {
		code = CodeParsingError
	}
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// CodeOf extracts the standardized code from err, falling back to
// PARSING_ERROR for plain Go errors.
func CodeOf(err error) ErrorCode {
	var coded *Error
	if errors.As(err, &coded) {
		return coded.Code
	}
	return CodeParsingError
}

// codeForStatus maps an upstream HTTP status onto a standardized code. It is
// used by host-side pipelines such as registry downloads and image delivery
// that own the request; responses handed to makinuki_fetch keep their raw
// status so the plugin performs its own mapping.
func codeForStatus(status int) ErrorCode {
	switch {
	case status == http.StatusUnauthorized:
		return CodeSessionRequired
	case status == http.StatusForbidden:
		return CodeCloudflareBlocked
	case status == http.StatusNotFound:
		return CodeNotFound
	case status == http.StatusTooManyRequests:
		return CodeRateLimited
	case status == http.StatusServiceUnavailable:
		return CodeCloudflareBlocked
	case status >= 500:
		return CodeSourceOffline
	default:
		return CodeSourceOffline
	}
}

// HTTPStatusFor maps a standardized code onto the status the local REST API
// reports to the web UI.
func HTTPStatusFor(code ErrorCode) int {
	switch code {
	case CodeNotFound:
		return http.StatusNotFound
	case CodeRateLimited:
		return http.StatusTooManyRequests
	case CodeSessionRequired, CodeAuthExpired:
		return http.StatusUnauthorized
	case CodeCloudflareBlocked:
		return http.StatusForbidden
	case CodeNetworkTimeout:
		return http.StatusGatewayTimeout
	case CodeSourceOffline:
		return http.StatusBadGateway
	case CodeMemoryLimitExceeded:
		return http.StatusRequestEntityTooLarge
	case CodeUnsupportedMedia:
		return http.StatusUnsupportedMediaType
	default:
		return http.StatusBadGateway
	}
}
