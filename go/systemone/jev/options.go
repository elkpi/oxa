package jev

import "github.com/elkpi/oxa-protocol/go/v3/modelmap"

// options carries per-converter configuration. The package-level conversion
// functions accept variadic Options; with none supplied the model table
// defaults to the identity (spec/03 s2).
type options struct {
	models    modelmap.Table
	questions map[string]Question
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

// WithQuestions installs the original request's questions as response-encode
// context (spec/17 §4.1): answer types recover exactly from the matching
// question instead of the value-shape inference, and criteria ranges are
// validated.
func WithQuestions(qs map[string]Question) Option {
	return func(o *options) {
		o.questions = qs
	}
}

func newOptions(opts ...Option) *options {
	o := &options{}
	for _, opt := range opts {
		opt(o)
	}
	return o
}
