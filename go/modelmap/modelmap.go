// Package modelmap implements the single, optional model-renaming injection
// point defined by spec/03. oxa libraries carry no built-in model knowledge;
// callers may supply a Table, and the table is applied to the model value on
// both conversion directions. The model string is otherwise opaque and passes
// through verbatim.
package modelmap

// Table maps model names to model names. Lookup is exact-match on the keys;
// on a miss (or with a nil or empty table) the identity fallback applies and
// the value is returned unchanged. No table installed is exactly an empty
// table.
type Table map[string]string

// Map returns the table entry for m, or m unchanged when there is none. A nil
// Table is safe to call.
func (t Table) Map(m string) string {
	if t == nil {
		return m
	}
	if v, ok := t[m]; ok {
		return v
	}
	return m
}
