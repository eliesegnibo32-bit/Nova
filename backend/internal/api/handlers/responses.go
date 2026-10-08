// Package handlers groups all HTTP handler functions for the NOVA API.
package handlers

import (
        "encoding/json"
        "errors"
        "net/http"
)

// APIVersion is the public API version reported by /health and embedded in
// any future versioned error responses.
const APIVersion = "1.0.0"

// errorEnvelope is the canonical JSON error shape returned by all handlers.
//
//      {
//        "error":   "not_found",        // stable machine code
//        "message": "Shop not found."   // human-friendly detail
//      }
type errorEnvelope struct {
        Error   string `json:"error"`
        Message string `json:"message"`
}

// errorEnvelopeWithCode is the richer shape used when an additional
// machine-readable sub-code is useful (e.g. validation failures listing
// the offending field). It is wire-compatible with errorEnvelope: clients
// that only read `error` and `message` still work; clients that know about
// `details` get extra context.
type errorEnvelopeWithCode struct {
        Error   string            `json:"error"`
        Message string            `json:"message"`
        Details map[string]string `json:"details,omitempty"`
}

// respondJSON serializes v as JSON and writes it with the given status. It
// sets the Content-Type header and never writes a body for 204 responses.
func respondJSON(w http.ResponseWriter, status int, v any) {
        if status == http.StatusNoContent {
                w.WriteHeader(status)
                return
        }
        w.Header().Set("Content-Type", "application/json; charset=utf-8")
        w.WriteHeader(status)
        if v != nil {
                _ = json.NewEncoder(w).Encode(v)
        }
}

// writeJSON is the public alias for respondJSON — exposed so handlers in
// other files (and tests) can call it without reaching into the private
// helper. Status is set first, then the JSON body is encoded.
func writeJSON(w http.ResponseWriter, status int, v any) {
        respondJSON(w, status, v)
}

// writeError writes a JSON error envelope with the given status and human
// message. The error code is derived from the standard http.StatusText —
// callers who want a custom code should use writeErrorWithCode.
func writeError(w http.ResponseWriter, status int, message string) {
        respondJSON(w, status, errorEnvelope{
                Error:   http.StatusText(status),
                Message: message,
        })
}

// writeErrorWithCode writes a JSON error envelope with a stable machine
// code (e.g. "invalid_credentials", "account_locked") plus a human
// message. Use this for all auth/business-logic errors so the frontend can
// branch on the code without parsing the message.
func writeErrorWithCode(w http.ResponseWriter, status int, code, message string) {
        respondJSON(w, status, errorEnvelope{Error: code, Message: message})
}

// writeErrorWithDetails is like writeErrorWithCode but additionally
// includes a `details` map for field-level validation errors. The keys are
// field paths (e.g. "email", "password") and the values are the validation
// messages. The HTTP status is typically 422 Unprocessable Entity.
func writeErrorWithDetails(w http.ResponseWriter, status int, code, message string, details map[string]string) {
        respondJSON(w, status, errorEnvelopeWithCode{
                Error:   code,
                Message: message,
                Details: details,
        })
}

// respondError is the legacy private alias kept for backwards compatibility
// with the existing handlers (shops.go, etc.). New code should call
// writeErrorWithCode.
func respondError(w http.ResponseWriter, status int, code, message string) {
        respondJSON(w, status, errorEnvelope{Error: code, Message: message})
}

// decodeJSON decodes the request body into v. It rejects bodies that
// contain unknown fields (DisallowUnknownFields) so clients get an early
// error instead of silently dropping fields.
func decodeJSON(r *http.Request, v any) error {
        dec := json.NewDecoder(r.Body)
        dec.DisallowUnknownFields()
        if err := dec.Decode(v); err != nil {
                return err
        }
        // Reject trailing garbage.
        if dec.More() {
                return errTrailingJSON
        }
        return nil
}

// errTrailingJSON is returned by decodeJSON when the request body contains
// more than one JSON value (a sign of client bug or attack).
var errTrailingJSON = errors.New("request body must contain a single JSON object")
