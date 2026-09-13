// Package diff compares two environments key by key, ignoring file order.
package diff

import "slices"

// Vars is the read-only view of an environment that Compare needs.
type Vars interface {
	// Keys returns every variable name, in any order.
	Keys() []string
	// Lookup returns the value for key and whether the key exists.
	Lookup(key string) (string, bool)
}

// Result classifies every key from both environments. Each slice is sorted
// and never nil. Values are not retained.
type Result struct {
	Same      []string // in both with equal values, or in both when KeysOnly
	Missing   []string // in source only
	Extra     []string // in target only
	Different []string // in both with unequal values; always empty when KeysOnly
	KeysOnly  bool
}

// Count returns the number of keys that drifted.
func (x Result) Count() int {
	return len(x.Missing) + len(x.Extra) + len(x.Different)
}

// HasDrift reports whether any key is missing, extra, or different.
func (x Result) HasDrift() bool {
	return x.Count() > 0
}

// Compare classifies the keys of source against target. With keysOnly,
// values are ignored and a key present in both is reported as Same.
func Compare(source, target Vars, keysOnly bool) Result {
	r := Result{
		Same:      []string{},
		Missing:   []string{},
		Extra:     []string{},
		Different: []string{},
		KeysOnly:  keysOnly,
	}
	for _, key := range slices.Sorted(slices.Values(source.Keys())) {
		sv, _ := source.Lookup(key)
		tv, ok := target.Lookup(key)
		switch {
		case !ok:
			r.Missing = append(r.Missing, key)
		case keysOnly || sv == tv:
			r.Same = append(r.Same, key)
		default:
			r.Different = append(r.Different, key)
		}
	}
	for _, key := range slices.Sorted(slices.Values(target.Keys())) {
		if _, ok := source.Lookup(key); !ok {
			r.Extra = append(r.Extra, key)
		}
	}
	return r
}
