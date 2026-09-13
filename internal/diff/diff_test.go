package diff

import (
	"maps"
	"slices"
	"testing"
)

type vars map[string]string

func (v vars) Keys() []string { return slices.Collect(maps.Keys(v)) }

func (v vars) Lookup(key string) (string, bool) {
	s, ok := v[key]
	return s, ok
}

func TestCompare(t *testing.T) {
	tests := []struct {
		name         string
		source       vars
		target       vars
		keysOnly     bool
		same         []string
		missing      []string
		extra        []string
		different    []string
		wantHasDrift bool
		wantCount    int
	}{
		{
			name:   "identical",
			source: vars{"A": "1", "B": "2"},
			target: vars{"B": "2", "A": "1"},
			same:   []string{"A", "B"},
		},
		{
			name:         "missing",
			source:       vars{"A": "1", "B": "2"},
			target:       vars{"A": "1"},
			same:         []string{"A"},
			missing:      []string{"B"},
			wantHasDrift: true, wantCount: 1,
		},
		{
			name:         "extra",
			source:       vars{"A": "1"},
			target:       vars{"A": "1", "B": "2"},
			same:         []string{"A"},
			extra:        []string{"B"},
			wantHasDrift: true, wantCount: 1,
		},
		{
			name:         "different",
			source:       vars{"A": "1", "B": "2"},
			target:       vars{"A": "1", "B": "3"},
			same:         []string{"A"},
			different:    []string{"B"},
			wantHasDrift: true, wantCount: 1,
		},
		{
			name:         "empty value differs from a set value",
			source:       vars{"A": ""},
			target:       vars{"A": "x"},
			different:    []string{"A"},
			wantHasDrift: true, wantCount: 1,
		},
		{
			name:         "mixed",
			source:       vars{"DATABASE_URL": "x", "REDIS_URL": "y", "STRIPE_API_KEY": "z", "LOG_LEVEL": "debug"},
			target:       vars{"DATABASE_URL": "x", "REDIS_URL": "y", "LOG_LEVEL": "info", "OLD_FEATURE_FLAG": "1"},
			same:         []string{"DATABASE_URL", "REDIS_URL"},
			missing:      []string{"STRIPE_API_KEY"},
			extra:        []string{"OLD_FEATURE_FLAG"},
			different:    []string{"LOG_LEVEL"},
			wantHasDrift: true, wantCount: 3,
		},
		{
			name:         "keys only ignores values",
			source:       vars{"A": "1", "B": "2"},
			target:       vars{"A": "9", "C": "3"},
			keysOnly:     true,
			same:         []string{"A"},
			missing:      []string{"B"},
			extra:        []string{"C"},
			wantHasDrift: true, wantCount: 2,
		},
		{
			name:         "empty target",
			source:       vars{"A": "1", "B": "2"},
			target:       vars{},
			missing:      []string{"A", "B"},
			wantHasDrift: true, wantCount: 2,
		},
		{
			name:         "empty source",
			source:       vars{},
			target:       vars{"A": "1"},
			extra:        []string{"A"},
			wantHasDrift: true, wantCount: 1,
		},
		{
			name:   "both empty",
			source: vars{},
			target: vars{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Compare(tt.source, tt.target, tt.keysOnly)
			check := func(field string, got, want []string) {
				t.Helper()
				if got == nil {
					t.Errorf("%s is nil, want an empty slice", field)
				}
				if want == nil {
					want = []string{}
				}
				if !slices.Equal(got, want) {
					t.Errorf("%s = %v, want %v", field, got, want)
				}
			}
			check("Same", r.Same, tt.same)
			check("Missing", r.Missing, tt.missing)
			check("Extra", r.Extra, tt.extra)
			check("Different", r.Different, tt.different)
			if r.HasDrift() != tt.wantHasDrift {
				t.Errorf("HasDrift() = %v, want %v", r.HasDrift(), tt.wantHasDrift)
			}
			if r.Count() != tt.wantCount {
				t.Errorf("Count() = %d, want %d", r.Count(), tt.wantCount)
			}
			if r.KeysOnly != tt.keysOnly {
				t.Errorf("KeysOnly = %v, want %v", r.KeysOnly, tt.keysOnly)
			}
		})
	}
}

func TestCompareSortsKeys(t *testing.T) {
	r := Compare(vars{"z": "", "a": "", "m": ""}, vars{"y": "", "b": ""}, false)
	if want := []string{"a", "m", "z"}; !slices.Equal(r.Missing, want) {
		t.Errorf("Missing = %v, want %v", r.Missing, want)
	}
	if want := []string{"b", "y"}; !slices.Equal(r.Extra, want) {
		t.Errorf("Extra = %v, want %v", r.Extra, want)
	}
}
