package telconyx

import (
	"errors"
	"fmt"
	"time"
)

var (
	// ErrFileTooBig indicates a single chunk exceeds the Bot API limit (50 MB).
	// This is internal; user-facing errors should be ErrUploadTooLarge or ErrDownloadTooLarge.
	ErrFileTooBig = errors.New("telconyx: chunk exceeds Bot API limit (50 MB)")
	// ErrUploadTooLarge indicates the file exceeds the configured MaxUploadSize.
	ErrUploadTooLarge = errors.New("telconyx: file exceeds MaxUploadSize")
	// ErrDownloadTooLarge indicates the file exceeds the configured MaxDownloadSize.
	ErrDownloadTooLarge = errors.New("telconyx: file exceeds MaxDownloadSize")
	ErrInvalidConfig    = errors.New("telconyx: invalid config")
	ErrInvalidLink      = errors.New("telconyx: invalid telconyx:// link")
	ErrUnauthorized     = errors.New("telconyx: unauthorized")
)

// APIError represents a non-success response from the Telegram Bot API.
type APIError struct {
	Code        int
	Description string
	Method      string
}

func (e *APIError) Error() string {
	if e.Method != "" {
		return fmt.Sprintf("telconyx: %s: api error %d: %s", e.Method, e.Code, e.Description)
	}
	return fmt.Sprintf("telconyx: api error %d: %s", e.Code, e.Description)
}

// NonRetryableError signals that a request will keep failing no matter how
// many times it is retried (e.g. the server returned a malformed response,
// the chat rejected the file, or the bot lacks permission). Returning it
// from an upload/download step short-circuits withRetry to avoid producing
// duplicate uploads in the target chat.
type NonRetryableError struct {
	Method string
	Reason string
	// Detail is an optional snippet of the server response for debugging.
	Detail string
}

func (e *NonRetryableError) Error() string {
	switch {
	case e.Method != "" && e.Detail != "":
		return fmt.Sprintf("telconyx: %s: %s (non-retryable; server said: %s)", e.Method, e.Reason, e.Detail)
	case e.Method != "":
		return fmt.Sprintf("telconyx: %s: %s (non-retryable)", e.Method, e.Reason)
	default:
		return fmt.Sprintf("telconyx: %s (non-retryable)", e.Reason)
	}
}

// FloodWaitError indicates a rate-limit response (HTTP 429) with retry_after.
type FloodWaitError struct {
	Seconds int
}

func (e *FloodWaitError) Error() string {
	return fmt.Sprintf("telconyx: flood wait %ds", e.Seconds)
}

// Duration returns the suggested wait time as a time.Duration.
func (e *FloodWaitError) Duration() time.Duration {
	if e.Seconds <= 0 {
		return 0
	}
	return time.Duration(e.Seconds) * time.Second
}
