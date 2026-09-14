// Package diff compares two or more environments key by key, ignoring file
// order. The first environment is the reference the others are judged
// against.
package diff

import (
	"maps"
	"slices"
)

// Vars is the read-only view of an environment that Compare needs.
type Vars interface {
	// Keys returns every variable name, in any order.
	Keys() []string
	// Lookup returns the value for key and whether the key exists.
	Lookup(key string) (string, bool)
}

// Environment is a named set of variables, usually one file.
type Environment struct {
	Name string
	Vars Vars
}

// Options tunes how keys are classified.
type Options struct {
	// KeysOnly ignores values: a key present everywhere is Same.
	KeysOnly bool
	// Ignore names keys to leave out of the comparison entirely.
	Ignore []string
	// AllowExtra keeps Extra keys out of Count and HasDrift. They are still
	// listed so reports can mention them.
	AllowExtra bool
}

// Status classifies one key across all environments.
type Status int

// Statuses, in the order the terminal report lists them.
const (
	Same      Status = iota // present everywhere with equal values
	Missing                 // present in the reference, absent from another
	Extra                   // absent from the reference, present in another
	Different               // present everywhere, values not all equal
)

func (s Status) String() string {
	switch s {
	case Same:
		return "same"
	case Missing:
		return "missing"
	case Extra:
		return "extra"
	case Different:
		return "different"
	}
	return "unknown"
}

// Cell is one key in one environment. Present cells with equal values share
// a Group number, assigned in environment order starting at 0; an absent
// cell has Group -1.
type Cell struct {
	Present bool
	Group   int
}

// Row is one key across all environments, cells in environment order.
type Row struct {
	Key    string
	Status Status
	Cells  []Cell
}

// Groups returns how many distinct values the present cells hold.
func (x Row) Groups() int {
	n := 0
	for _, c := range x.Cells {
		n = max(n, c.Group+1)
	}
	return n
}

// Result classifies every key from every environment. Rows and the status
// lists are sorted by key and never nil. Values are not retained.
type Result struct {
	Envs      []string // names in input order; Envs[0] is the reference
	Rows      []Row    // every key
	Same      []string
	Missing   []string
	Extra     []string
	Different []string
	Ignored   []string // ignored keys that exist in at least one environment
	Options   Options
}

// Count returns the number of keys that drifted. Extra keys do not count
// when Options.AllowExtra is set.
func (x Result) Count() int {
	n := len(x.Missing) + len(x.Different)
	if !x.Options.AllowExtra {
		n += len(x.Extra)
	}
	return n
}

// HasDrift reports whether any key is missing, extra, or different.
func (x Result) HasDrift() bool {
	return x.Count() > 0
}

// Compare classifies the keys of envs against the first environment. Keys
// named in opts.Ignore are skipped and listed in Result.Ignored when they
// exist somewhere.
// Presence is judged before values: a key absent somewhere is Missing or
// Extra even if the present values also disagree; its cells still show the
// value groups.
func Compare(envs []Environment, opts Options) Result {
	r := Result{
		Envs:      make([]string, 0, len(envs)),
		Rows:      []Row{},
		Same:      []string{},
		Missing:   []string{},
		Extra:     []string{},
		Different: []string{},
		Ignored:   []string{},
		Options:   opts,
	}
	ignore := map[string]bool{}
	for _, k := range opts.Ignore {
		ignore[k] = true
	}
	keys := map[string]struct{}{}
	for _, e := range envs {
		r.Envs = append(r.Envs, e.Name)
		for _, k := range e.Vars.Keys() {
			keys[k] = struct{}{}
		}
	}
	for _, key := range slices.Sorted(maps.Keys(keys)) {
		if ignore[key] {
			r.Ignored = append(r.Ignored, key)
			continue
		}
		row := classify(key, envs, opts.KeysOnly)
		r.Rows = append(r.Rows, row)
		switch row.Status {
		case Same:
			r.Same = append(r.Same, key)
		case Missing:
			r.Missing = append(r.Missing, key)
		case Extra:
			r.Extra = append(r.Extra, key)
		case Different:
			r.Different = append(r.Different, key)
		}
	}
	return r
}

func classify(key string, envs []Environment, keysOnly bool) Row {
	row := Row{Key: key, Cells: make([]Cell, len(envs))}
	groups := map[string]int{} // value -> group; lives only for this call
	present := 0
	for i, e := range envs {
		v, ok := e.Vars.Lookup(key)
		if !ok {
			row.Cells[i] = Cell{Group: -1}
			continue
		}
		present++
		if keysOnly {
			v = ""
		}
		g, seen := groups[v]
		if !seen {
			g = len(groups)
			groups[v] = g
		}
		row.Cells[i] = Cell{Present: true, Group: g}
	}
	switch {
	case present == len(envs) && len(groups) <= 1:
		row.Status = Same
	case present == len(envs):
		row.Status = Different
	case len(envs) > 0 && row.Cells[0].Present:
		row.Status = Missing
	default:
		row.Status = Extra
	}
	return row
}
