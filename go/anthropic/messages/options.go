package messages

import "github.com/oxa-protocol/oxa/go/modelmap"

// options carries per-converter configuration. In this milestone the package
// exposes the four package-level conversion functions with fixed signatures,
// so the model table defaults to the identity (no table installed is exactly
// an empty table, spec/03 s2); Option and WithModelMap define the injection
// surface the client API (a later milestone) attaches per-converter tables to.
type options struct {
	models modelmap.Table
}

// Option configures a converter.
type Option func(*options)

// WithModelMap installs a caller-supplied model-name table (spec/03 s2).
// Lookup is exact-match with identity fallback; the table applies on both
// conversion directions.
func WithModelMap(t modelmap.Table) Option {
	return func(o *options) {
		o.models = t
	}
}

func newOptions(opts ...Option) *options {
	o := &options{}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// defaultOptions is the identity default used by the package-level
// conversion functions: no table, the model string passes through verbatim.
var defaultOptions = newOptions()
