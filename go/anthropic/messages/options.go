package messages

import "github.com/elkpi/oxa/go/modelmap"

// options carries per-converter configuration. The package-level conversion
// functions accept variadic Options; with none supplied the model table
// defaults to the identity (no table installed is exactly an empty table,
// spec/03 s2).
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
