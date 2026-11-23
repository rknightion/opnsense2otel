package opnsense

import (
	"encoding/json"
	"errors"
	"fmt"
)

// APICallError is an error returned by the OPNsense API
type APICallError struct {
	Endpoint   string
	Message    string
	StatusCode int

	// PartialDecode is true when the body was valid JSON whose SHAPE
	// disagreed with the target struct on one or more fields — encoding/json's
	// UnmarshalTypeError. The decoder records a type error and keeps going, so
	// by the time it returns, every field it did understand is already written
	// to the caller's struct: the data is usable, minus the fields that
	// mismatched.
	//
	// This exists so a caller that can degrade gracefully has the option, and
	// does not throw a whole payload away over one bad field. That is exactly
	// what #615 did — two mismodelled wear percentages made the entire
	// smartInfo body undecodable, which cost every per-device SMART metric on
	// the only box with a real disk.
	//
	// A malformed body (not JSON at all, or a shape mismatch at the very top
	// level) is NOT partial: nothing decoded, so there is nothing to keep.
	PartialDecode bool
}

// httpFailureMessage is the Message of a non-2xx response. It names the status and
// nothing the upstream sent: an error body can quote credentials or voucher codes,
// and this field reaches the logs. It omits the request path too, which can hold
// identifiers a caller removed from Endpoint; Error prints Endpoint.
func httpFailureMessage(statusCode int) string {
	return fmt.Sprintf("HTTP %d (response body withheld)", statusCode)
}

// decodeFailureMessage is the Message of a response that could not be decoded.
func decodeFailureMessage(statusCode int, err error) string {
	return fmt.Sprintf("failed to unmarshal response body: HTTP %d: %s (content withheld)",
		statusCode, decodeFailureReason(err))
}

// decodeFailureReason classifies a decoder error without its text, which can quote
// bytes of the document: a number literal, a character, or whatever a custom
// unmarshaler put in its own error.
func decodeFailureReason(err error) string {
	var typeErr *json.UnmarshalTypeError
	var syntaxErr *json.SyntaxError
	switch {
	case errors.As(err, &typeErr):
		return "response did not match the expected schema"
	case errors.As(err, &syntaxErr):
		return "response is not valid JSON"
	default:
		return "response could not be decoded"
	}
}

func (e APICallError) Error() string {
	return fmt.Sprintf(
		"opnsense-client api call error: endpoint: %s; failed status code: %d; msg: %s", e.Endpoint, e.StatusCode, e.Message,
	)
}
