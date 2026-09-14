package diff

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"testing"
)

type vars map[string]string

func (v vars) Keys() []string { return slices.Collect(maps.Keys(v)) }

func (v vars) Lookup(key string) (string, bool) {
	s, ok := v[key]
	return s, ok
}

func envs(vs ...vars) []Environment {
	out := make([]Environment, len(vs))
	for i, v := range vs {
		out[i] = Environment{Name: fmt.Sprintf("env%d", i), Vars: v}
	}
	return out
}

func TestCompare(t *testing.T) {
	tests := []struct {
		name         string
		envs         []Environment
		keysOnly     bool
		same         []string
		missing      []string
		extra        []string
		different    []string
		wantHasDrift bool
		wantCount    int
	}{
		{
			name: "identical",
			envs: envs(vars{"A": "1", "B": "2"}, vars{"B": "2", "A": "1"}),
			same: []string{"A", "B"},
		},
		{
			name:         "missing",
			envs:         envs(vars{"A": "1", "B": "2"}, vars{"A": "1"}),
			same:         []string{"A"},
			missing:      []string{"B"},
			wantHasDrift: true, wantCount: 1,
		},
		{
			name:         "extra",
			envs:         envs(vars{"A": "1"}, vars{"A": "1", "B": "2"}),
			same:         []string{"A"},
			extra:        []string{"B"},
			wantHasDrift: true, wantCount: 1,
		},
		{
			name:         "different",
			envs:         envs(vars{"A": "1", "B": "2"}, vars{"A": "1", "B": "3"}),
			same:         []string{"A"},
			different:    []string{"B"},
			wantHasDrift: true, wantCount: 1,
		},
		{
			name:         "empty value differs from a set value",
			envs:         envs(vars{"A": ""}, vars{"A": "x"}),
			different:    []string{"A"},
			wantHasDrift: true, wantCount: 1,
		},
		{
			name: "mixed",
			envs: envs(
				vars{"DATABASE_URL": "x", "REDIS_URL": "y", "STRIPE_API_KEY": "z", "LOG_LEVEL": "debug"},
				vars{"DATABASE_URL": "x", "REDIS_URL": "y", "LOG_LEVEL": "info", "OLD_FEATURE_FLAG": "1"},
			),
			same:         []string{"DATABASE_URL", "REDIS_URL"},
			missing:      []string{"STRIPE_API_KEY"},
			extra:        []string{"OLD_FEATURE_FLAG"},
			different:    []string{"LOG_LEVEL"},
			wantHasDrift: true, wantCount: 3,
		},
		{
			name:         "keys only ignores values",
			envs:         envs(vars{"A": "1", "B": "2"}, vars{"A": "9", "C": "3"}),
			keysOnly:     true,
			same:         []string{"A"},
			missing:      []string{"B"},
			extra:        []string{"C"},
			wantHasDrift: true, wantCount: 2,
		},
		{
			name:         "empty target",
			envs:         envs(vars{"A": "1", "B": "2"}, vars{}),
			missing:      []string{"A", "B"},
			wantHasDrift: true, wantCount: 2,
		},
		{
			name:         "empty source",
			envs:         envs(vars{}, vars{"A": "1"}),
			extra:        []string{"A"},
			wantHasDrift: true, wantCount: 1,
		},
		{
			name: "both empty",
			envs: envs(vars{}, vars{}),
		},
		{
			name: "three identical",
			envs: envs(vars{"A": "1"}, vars{"A": "1"}, vars{"A": "1"}),
			same: []string{"A"},
		},
		{
			name:         "missing in one of two targets",
			envs:         envs(vars{"A": "1"}, vars{}, vars{"A": "1"}),
			missing:      []string{"A"},
			wantHasDrift: true, wantCount: 1,
		},
		{
			name:         "extra in one of two targets",
			envs:         envs(vars{}, vars{"A": "1"}, vars{}),
			extra:        []string{"A"},
			wantHasDrift: true, wantCount: 1,
		},
		{
			name:         "different in one of two targets",
			envs:         envs(vars{"A": "1"}, vars{"A": "2"}, vars{"A": "1"}),
			different:    []string{"A"},
			wantHasDrift: true, wantCount: 1,
		},
		{
			name:         "absence wins over a value difference",
			envs:         envs(vars{"A": "1"}, vars{}, vars{"A": "2"}),
			missing:      []string{"A"},
			wantHasDrift: true, wantCount: 1,
		},
		{
			name:         "extra with disagreeing targets is still extra",
			envs:         envs(vars{}, vars{"A": "1"}, vars{"A": "2"}),
			extra:        []string{"A"},
			wantHasDrift: true, wantCount: 1,
		},
		{
			name:         "keys only across three",
			envs:         envs(vars{"A": "1", "B": "1"}, vars{"A": "2"}, vars{"A": "3", "B": "2"}),
			keysOnly:     true,
			same:         []string{"A"},
			missing:      []string{"B"},
			wantHasDrift: true, wantCount: 1,
		},
		{
			name: "single environment has nothing to drift from",
			envs: envs(vars{"A": "1"}),
			same: []string{"A"},
		},
		{
			name: "no environments",
			envs: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Compare(tt.envs, Options{KeysOnly: tt.keysOnly})
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
			if r.Options.KeysOnly != tt.keysOnly {
				t.Errorf("Options.KeysOnly = %v, want %v", r.Options.KeysOnly, tt.keysOnly)
			}
			if r.Ignored == nil || len(r.Ignored) != 0 {
				t.Errorf("Ignored = %v, want an empty slice", r.Ignored)
			}
			if len(r.Rows) != len(tt.same)+len(tt.missing)+len(tt.extra)+len(tt.different) {
				t.Errorf("Rows has %d entries, want one per key", len(r.Rows))
			}
			if r.Envs == nil || r.Rows == nil {
				t.Error("Envs or Rows is nil")
			}
		})
	}
}

func TestCompareRows(t *testing.T) {
	r := Compare([]Environment{
		{Name: "dev", Vars: vars{"A": "x", "B": "x", "C": "x"}},
		{Name: "staging", Vars: vars{"A": "y", "C": "x"}},
		{Name: "prod", Vars: vars{"A": "x", "B": "y", "C": "x", "D": "q"}},
	}, Options{})

	if want := []string{"dev", "staging", "prod"}; !slices.Equal(r.Envs, want) {
		t.Errorf("Envs = %v, want %v", r.Envs, want)
	}
	want := []Row{
		{Key: "A", Status: Different, Cells: []Cell{{true, 0}, {true, 1}, {true, 0}}},
		{Key: "B", Status: Missing, Cells: []Cell{{true, 0}, {false, -1}, {true, 1}}},
		{Key: "C", Status: Same, Cells: []Cell{{true, 0}, {true, 0}, {true, 0}}},
		{Key: "D", Status: Extra, Cells: []Cell{{false, -1}, {false, -1}, {true, 0}}},
	}
	if !reflect.DeepEqual(r.Rows, want) {
		t.Errorf("Rows =\n%+v\nwant:\n%+v", r.Rows, want)
	}
	for i, groups := range []int{2, 2, 1, 1} {
		if got := r.Rows[i].Groups(); got != groups {
			t.Errorf("Rows[%d].Groups() = %d, want %d", i, got, groups)
		}
	}
}

func TestCompareGroupsWhenReferenceLacksKey(t *testing.T) {
	r := Compare(envs(vars{}, vars{"A": "p"}, vars{"A": "q"}, vars{"A": "p"}), Options{})
	want := []Cell{{false, -1}, {true, 0}, {true, 1}, {true, 0}}
	if !reflect.DeepEqual(r.Rows[0].Cells, want) {
		t.Errorf("Cells = %+v, want %+v", r.Rows[0].Cells, want)
	}
}

func TestCompareKeysOnlyCollapsesGroups(t *testing.T) {
	r := Compare(envs(vars{"A": "1"}, vars{"A": "2"}, vars{"A": "3"}), Options{KeysOnly: true})
	want := []Cell{{true, 0}, {true, 0}, {true, 0}}
	if !reflect.DeepEqual(r.Rows[0].Cells, want) {
		t.Errorf("Cells = %+v, want %+v", r.Rows[0].Cells, want)
	}
	if r.Rows[0].Status != Same {
		t.Errorf("Status = %v, want same", r.Rows[0].Status)
	}
}

func TestCompareIgnore(t *testing.T) {
	r := Compare(envs(
		vars{"A": "1", "B": "1", "D": "1"},
		vars{"A": "1", "B": "2", "C": "1", "E": "1"},
	), Options{Ignore: []string{"E", "B", "ZZZ"}})

	if want := []string{"B", "E"}; !slices.Equal(r.Ignored, want) {
		t.Errorf("Ignored = %v, want %v (sorted, only keys that exist)", r.Ignored, want)
	}
	if !slices.Equal(r.Same, []string{"A"}) || !slices.Equal(r.Missing, []string{"D"}) ||
		!slices.Equal(r.Extra, []string{"C"}) || len(r.Different) != 0 {
		t.Errorf("lists = %v/%v/%v/%v", r.Same, r.Missing, r.Extra, r.Different)
	}
	for _, row := range r.Rows {
		if row.Key == "B" || row.Key == "E" {
			t.Errorf("Rows still contains ignored key %s", row.Key)
		}
	}
	if !slices.Equal(r.Options.Ignore, []string{"E", "B", "ZZZ"}) {
		t.Errorf("Options.Ignore = %v, want the flag echoed", r.Options.Ignore)
	}
	if r.Count() != 2 {
		t.Errorf("Count() = %d, want 2", r.Count())
	}
}

func TestCompareAllowExtra(t *testing.T) {
	e := envs(vars{"A": "1", "B": "1"}, vars{"A": "1", "C": "1", "D": "1"})
	strict := Compare(e, Options{})
	lenient := Compare(e, Options{AllowExtra: true})

	if strict.Count() != 3 || !strict.HasDrift() {
		t.Errorf("strict: Count = %d, HasDrift = %v", strict.Count(), strict.HasDrift())
	}
	if lenient.Count() != 1 || !lenient.HasDrift() {
		t.Errorf("lenient: Count = %d (missing B only), HasDrift = %v", lenient.Count(), lenient.HasDrift())
	}
	if !slices.Equal(lenient.Extra, []string{"C", "D"}) {
		t.Errorf("lenient.Extra = %v, want the extras still listed", lenient.Extra)
	}

	onlyExtra := Compare(envs(vars{"A": "1"}, vars{"A": "1", "C": "1"}), Options{AllowExtra: true})
	if onlyExtra.HasDrift() || onlyExtra.Count() != 0 {
		t.Errorf("only extras: HasDrift = %v, Count = %d", onlyExtra.HasDrift(), onlyExtra.Count())
	}
}

func TestStatusString(t *testing.T) {
	for s, want := range map[Status]string{Same: "same", Missing: "missing", Extra: "extra", Different: "different", Status(99): "unknown"} {
		if got := s.String(); got != want {
			t.Errorf("Status(%d).String() = %q, want %q", s, got, want)
		}
	}
}
