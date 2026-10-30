// Validator singleton for request DTOs.
//
// We use a single shared *validator.Validate so the struct tags are parsed
// once at init time and reused for every request. The validator is the
// standard go-playground/validator/v10 — same lib Echo, Fiber, and Gin use
// under the hood, so frontend devs will recognize the tag syntax.
//
// Tags used in this codebase:
//
//	required            — non-zero value (after TrimSpace for strings, the
//	                      service layer trims; validator does not).
//	email               — RFC-ish email format.
//	min=N               — minimum length (strings) or value (numbers).
//	len=N               — exact length (strings).
//	omitempty           — skip validation when the field is the zero value.
package handlers

import "github.com/go-playground/validator/v10"

// validate is the package-level validator instance. validator.Validate is
// goroutine-safe after the first call to Struct for a given type (rules are
// compiled and cached on first use), so a single shared instance is fine.
var validate = validator.New()
