package presenter

import (
	"bytes"
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/blackhorseya/env-diff/internal/diff"
)

type vars map[string]string

func (v vars) Keys() []string { return slices.Collect(maps.Keys(v)) }

func (v vars) Lookup(key string) (string, bool) {
	s, ok := v[key]
	return s, ok
}

func sp(n int) string { return strings.Repeat(" ", n) }

// drift compares two environments that differ by one missing, one extra
// and two different keys.
func drift() diff.Result {
	return diff.Compare([]diff.Environment{
		{Name: ".env.staging", Vars: vars{"DATABASE_URL": "x", "REDIS_URL": "y", "STRIPE_API_KEY": "z", "LOG_LEVEL": "debug", "FEATURE_PAYMENT": "a"}},
		{Name: ".env.production", Vars: vars{"DATABASE_URL": "x", "REDIS_URL": "y", "LOG_LEVEL": "info", "FEATURE_PAYMENT": "b", "OLD_FEATURE_FLAG": "1"}},
	}, diff.Options{})
}

// three compares three environments; qa agrees with production on
// LOG_LEVEL and with staging on everything else.
func three() diff.Result {
	return diff.Compare([]diff.Environment{
		{Name: ".env.staging", Vars: vars{"DATABASE_URL": "x", "STRIPE_API_KEY": "z", "LOG_LEVEL": "debug"}},
		{Name: ".env.production", Vars: vars{"DATABASE_URL": "x", "LOG_LEVEL": "info", "OLD_FEATURE_FLAG": "1"}},
		{Name: ".env.qa", Vars: vars{"DATABASE_URL": "x", "STRIPE_API_KEY": "z", "LOG_LEVEL": "info"}},
	}, diff.Options{})
}

func TestTerminal(t *testing.T) {
	tests := []struct {
		name   string
		result diff.Result
		want   string
	}{
		{
			name:   "drift",
			result: drift(),
			want: "Environment Drift\n" +
				"\n" +
				"Missing in target\n" +
				"  STRIPE_API_KEY\n" +
				"\n" +
				"Extra in target\n" +
				"  OLD_FEATURE_FLAG\n" +
				"\n" +
				"Different values\n" +
				"  FEATURE_PAYMENT\n" +
				"  LOG_LEVEL\n" +
				"\n" +
				"4 differences found\n",
		},
		{
			name:   "no drift",
			result: diff.Result{Same: []string{"A"}},
			want:   "Environment Drift\n\nNo differences found\n",
		},
		{
			name:   "single difference omits empty sections",
			result: diff.Result{Different: []string{"LOG_LEVEL"}},
			want:   "Environment Drift\n\nDifferent values\n  LOG_LEVEL\n\n1 difference found\n",
		},
		{
			name:   "keys only",
			result: diff.Result{Missing: []string{"A"}, KeysOnly: true},
			want:   "Environment Drift (keys only)\n\nMissing in target\n  A\n\n1 difference found\n",
		},
		{
			name:   "matrix for three environments",
			result: three(),
			want: "Environment Drift\n" +
				"\n" +
				"KEY" + sp(15) + ".env.staging  .env.production  .env.qa\n" +
				"STRIPE_API_KEY" + sp(4) + "+" + sp(13) + "-" + sp(16) + "+" + sp(8) + "missing in .env.production\n" +
				"OLD_FEATURE_FLAG" + sp(2) + "-" + sp(13) + "+" + sp(16) + "-" + sp(8) + "extra in .env.production\n" +
				"LOG_LEVEL" + sp(9) + "a" + sp(13) + "b" + sp(16) + "b" + sp(8) + "different\n" +
				"\n" +
				"3 differences found\n",
		},
		{
			name: "matrix keys only never shows letters",
			result: diff.Compare([]diff.Environment{
				{Name: "a", Vars: vars{"K": "1", "M": "1"}},
				{Name: "b", Vars: vars{"K": "2"}},
				{Name: "c", Vars: vars{"K": "3", "M": "2"}},
			}, diff.Options{KeysOnly: true}),
			want: "Environment Drift (keys only)\n" +
				"\n" +
				"KEY  a  b  c\n" +
				"M    +  -  +  missing in b\n" +
				"\n" +
				"1 difference found\n",
		},
		{
			name: "matrix lists every environment a key is missing from",
			result: diff.Compare([]diff.Environment{
				{Name: "a", Vars: vars{"K": "1"}},
				{Name: "b", Vars: vars{}},
				{Name: "c", Vars: vars{}},
			}, diff.Options{}),
			want: "Environment Drift\n" +
				"\n" +
				"KEY  a  b  c\n" +
				"K    +  -  -  missing in b, c\n" +
				"\n" +
				"1 difference found\n",
		},
		{
			name: "matrix with no drift",
			result: diff.Compare([]diff.Environment{
				{Name: "a", Vars: vars{"K": "1"}}, {Name: "b", Vars: vars{"K": "1"}}, {Name: "c", Vars: vars{"K": "1"}},
			}, diff.Options{}),
			want: "Environment Drift\n\nNo differences found\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := Terminal(&buf, tt.result, false); err != nil {
				t.Fatal(err)
			}
			if got := buf.String(); got != tt.want {
				t.Errorf("Terminal() =\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

func TestSymbolBeyondLetters(t *testing.T) {
	row := diff.Row{Cells: []diff.Cell{{Present: true, Group: 0}, {Present: true, Group: 26}}}
	if got := symbol(row, row.Cells[1]); got != "26" {
		t.Errorf("symbol(group 26) = %q, want %q", got, "26")
	}
	if got := symbol(row, row.Cells[0]); got != "a" {
		t.Errorf("symbol(group 0) = %q, want %q", got, "a")
	}
}

func TestTerminalColor(t *testing.T) {
	for name, result := range map[string]diff.Result{"sections": drift(), "matrix": three()} {
		t.Run(name, func(t *testing.T) {
			var plain, colored bytes.Buffer
			if err := Terminal(&plain, result, false); err != nil {
				t.Fatal(err)
			}
			if err := Terminal(&colored, result, true); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(plain.String(), "\x1b[") {
				t.Error("plain output contains escape codes")
			}
			if !strings.Contains(colored.String(), "\x1b[") {
				t.Error("colored output has no escape codes")
			}
			// Stripping the codes must give back exactly the plain layout.
			stripped := colored.String()
			for _, code := range []string{ansiReset, ansiBold, ansiRed, ansiGreen, ansiYellow, ansiCyan} {
				stripped = strings.ReplaceAll(stripped, code, "")
			}
			if stripped != plain.String() {
				t.Errorf("colored output without codes =\n%s\nwant:\n%s", stripped, plain.String())
			}
		})
	}
}

type decoded struct {
	Source   string   `json:"source"`
	Target   string   `json:"target"`
	Envs     []string `json:"envs"`
	KeysOnly bool     `json:"keys_only"`
	Drift    bool     `json:"drift"`
	Summary  struct {
		Same, Missing, Extra, Different int
	} `json:"summary"`
	Missing, Extra, Different, Same []string
	Matrix                          map[string][]*int `json:"matrix"`
}

func TestJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := JSON(&buf, drift()); err != nil {
		t.Fatal(err)
	}
	var got decoded
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, buf.String())
	}
	if got.Source != ".env.staging" || got.Target != ".env.production" {
		t.Errorf("source/target = %q/%q", got.Source, got.Target)
	}
	if !slices.Equal(got.Envs, []string{".env.staging", ".env.production"}) {
		t.Errorf("envs = %v", got.Envs)
	}
	if !got.Drift || got.KeysOnly {
		t.Errorf("drift = %v, keys_only = %v", got.Drift, got.KeysOnly)
	}
	if got.Summary.Same != 2 || got.Summary.Missing != 1 || got.Summary.Extra != 1 || got.Summary.Different != 2 {
		t.Errorf("summary = %+v", got.Summary)
	}
	if !slices.Equal(got.Missing, []string{"STRIPE_API_KEY"}) || !slices.Equal(got.Extra, []string{"OLD_FEATURE_FLAG"}) ||
		!slices.Equal(got.Different, []string{"FEATURE_PAYMENT", "LOG_LEVEL"}) || !slices.Equal(got.Same, []string{"DATABASE_URL", "REDIS_URL"}) {
		t.Errorf("lists = %v/%v/%v/%v", got.Missing, got.Extra, got.Different, got.Same)
	}
	groups := func(key string) string {
		var parts []string
		for _, g := range got.Matrix[key] {
			if g == nil {
				parts = append(parts, "null")
			} else {
				parts = append(parts, string(rune('0'+*g)))
			}
		}
		return strings.Join(parts, ",")
	}
	for key, want := range map[string]string{
		"DATABASE_URL":     "0,0",
		"LOG_LEVEL":        "0,1",
		"OLD_FEATURE_FLAG": "null,0",
		"STRIPE_API_KEY":   "0,null",
	} {
		if got := groups(key); got != want {
			t.Errorf("matrix[%s] = %s, want %s", key, got, want)
		}
	}
	if len(got.Matrix) != 6 {
		t.Errorf("matrix has %d keys, want 6", len(got.Matrix))
	}
}

func TestJSONThreeEnvironments(t *testing.T) {
	var buf bytes.Buffer
	if err := JSON(&buf, three()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), `"target"`) {
		t.Errorf("target must be omitted for three environments:\n%s", buf.String())
	}
	var got decoded
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Source != ".env.staging" || len(got.Envs) != 3 {
		t.Errorf("source = %q, envs = %v", got.Source, got.Envs)
	}
	if g := got.Matrix["LOG_LEVEL"]; len(g) != 3 || *g[0] != 0 || *g[1] != 1 || *g[2] != 1 {
		t.Errorf("matrix[LOG_LEVEL] = %v", g)
	}
}

func TestJSONNeverNullLists(t *testing.T) {
	var buf bytes.Buffer
	if err := JSON(&buf, diff.Result{KeysOnly: true}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, field := range []string{`"envs": []`, `"missing": []`, `"extra": []`, `"different": []`, `"same": []`, `"matrix": {}`} {
		if !strings.Contains(out, field) {
			t.Errorf("output lacks %s:\n%s", field, out)
		}
	}
	if strings.Contains(out, "null") {
		t.Errorf("empty result contains null:\n%s", out)
	}
	if !strings.Contains(out, `"drift": false`) || !strings.Contains(out, `"keys_only": true`) {
		t.Errorf("output flags wrong:\n%s", out)
	}

	// With drift, null may appear only inside matrix entries.
	buf.Reset()
	if err := JSON(&buf, drift()); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"envs", "missing", "extra", "different", "same", "matrix", "target", "source"} {
		if strings.Contains(buf.String(), `"`+field+`": null`) {
			t.Errorf("%s is null:\n%s", field, buf.String())
		}
	}
}
