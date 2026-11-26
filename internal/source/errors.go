package source

import (
	"context"
	"errors"
	"strings"
	"time"
)

// ErrUnsupported is returned by optional Source methods when the source does
// not declare the corresponding capability.
var ErrUnsupported = errors.New("source: unsupported capability")

// ErrorCode classifies errors returned by a source so the host can react
// consistently (skipping unavailable episodes, cooling down on rate limits,
// marking deleted items, or pointing to plugin logs).
type ErrorCode string

const (
	ErrUnavailable ErrorCode = "unavailable"
	ErrRateLimited ErrorCode = "rateLimited"
	ErrNotFound    ErrorCode = "notFound"
	ErrNetwork     ErrorCode = "network"
	ErrPlugin      ErrorCode = "plugin"
)

// DefaultRetryAfter is the cooldown used for ErrRateLimited when the source
// does not specify RetryAfter.
const DefaultRetryAfter = 30 * time.Second

// Error is a structured error reported by a source or plugin.
type Error struct {
	Code       ErrorCode
	Message    string
	RetryAfter time.Duration
	Cause      error
}

// NewError builds a normalized *Error. Unknown error codes become ErrPlugin,
// and Message is trimmed and capped at MaxErrorMessageRunes.
func NewError(code ErrorCode, message string, cause error) *Error {
	return &Error{
		Code:    normalizeErrorCode(code),
		Message: truncateRunes(strings.TrimSpace(message), MaxErrorMessageRunes),
		Cause:   cause,
	}
}

// RateLimitedError builds an ErrRateLimited error with an optional retry delay.
func RateLimitedError(message string, retryAfter time.Duration, cause error) *Error {
	e := NewError(ErrRateLimited, message, cause)
	if retryAfter > 0 {
		e.RetryAfter = retryAfter
	}
	return e
}

func normalizeErrorCode(code ErrorCode) ErrorCode {
	switch code {
	case ErrUnavailable, ErrRateLimited, ErrNotFound, ErrNetwork, ErrPlugin:
		return code
	default:
		return ErrPlugin
	}
}

func (e *Error) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if def := DefaultMessage(e.Code); def != "" {
		return def
	}
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return string(e.Code)
}

func (e *Error) Unwrap() error { return e.Cause }

// DefaultMessage returns the default user-facing message for an ErrorCode.
func DefaultMessage(code ErrorCode) string {
	switch code {
	case ErrUnavailable:
		return "该内容暂不可播放"
	case ErrRateLimited:
		return "请求太频繁，请稍后再试"
	case ErrNotFound:
		return "内容不存在"
	case ErrNetwork:
		return "网络连接失败"
	case ErrPlugin:
		return "片源出错"
	default:
		return "片源出错"
	}
}

// CodeOf returns the ErrorCode of err: the code an *Error carries, even
// when its cause is a context error; "" for nil and for context.Canceled,
// which only the caller can tell apart from a failure (see Message); and
// ErrPlugin for anything else, a timeout included (§10).
func CodeOf(err error) ErrorCode {
	if err == nil {
		return ""
	}
	var se *Error
	if errors.As(err, &se) {
		return normalizeErrorCode(se.Code)
	}
	if errors.Is(err, context.Canceled) {
		return ""
	}
	return ErrPlugin
}

// Is reports whether err carries the given ErrorCode.
func Is(err error, code ErrorCode) bool {
	if err == nil || code == "" {
		return false
	}
	return CodeOf(err) == code
}

// RetryAfter returns how long the host should wait before retrying err, or 0
// when immediate retry is allowed.
func RetryAfter(err error) time.Duration {
	var se *Error
	if !errors.As(err, &se) {
		return 0
	}
	if se.RetryAfter > 0 {
		return se.RetryAfter
	}
	if normalizeErrorCode(se.Code) == ErrRateLimited {
		return DefaultRetryAfter
	}
	return 0
}

// Message turns err into a concise user-facing string. A call the host
// canceled is not an error and must not be shown, but only the caller knows
// whether it canceled: callers check their own context first. A source that
// reports context.Canceled on its own failed, so it gets the generic text.
func Message(err error) string {
	if err == nil {
		return ""
	}
	var se *Error
	if errors.As(err, &se) {
		return se.Error()
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "请求超时，请稍后再试"
	}
	if errors.Is(err, context.Canceled) {
		return DefaultMessage(ErrPlugin)
	}
	return truncateRunes(strings.TrimSpace(err.Error()), MaxErrorMessageRunes)
}
