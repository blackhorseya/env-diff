package presenter

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/blackhorseya/env-diff/internal/diff"
)

func drift() diff.Result {
	return diff.Result{
		Same:      []string{"DATABASE_URL", "REDIS_URL"},
		Missing:   []string{"STRIPE_API_KEY"},
		Extra:     []string{"OLD_FEATURE_FLAG"},
		Different: []string{"FEATURE_PAYMENT", "LOG_LEVEL"},
	}
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

func TestTerminalColor(t *testing.T) {
	var plain, colored bytes.Buffer
	if err := Terminal(&plain, drift(), false); err != nil {
		t.Fatal(err)
	}
	if err := Terminal(&colored, drift(), true); err != nil {
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
}

func TestJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := JSON(&buf, drift(), ".env.staging", ".env.production"); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Source   string `json:"source"`
		Target   string `json:"target"`
		KeysOnly bool   `json:"keys_only"`
		Drift    bool   `json:"drift"`
		Summary  struct {
			Same, Missing, Extra, Different int
		} `json:"summary"`
		Missing, Extra, Different, Same []string
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, buf.String())
	}
	if got.Source != ".env.staging" || got.Target != ".env.production" {
		t.Errorf("source/target = %q/%q", got.Source, got.Target)
	}
	if !got.Drift || got.KeysOnly {
		t.Errorf("drift = %v, keys_only = %v", got.Drift, got.KeysOnly)
	}
	if got.Summary.Same != 2 || got.Summary.Missing != 1 || got.Summary.Extra != 1 || got.Summary.Different != 2 {
		t.Errorf("summary = %+v", got.Summary)
	}
	if len(got.Missing) != 1 || got.Missing[0] != "STRIPE_API_KEY" {
		t.Errorf("missing = %v", got.Missing)
	}
	if len(got.Different) != 2 || len(got.Extra) != 1 || len(got.Same) != 2 {
		t.Errorf("different/extra/same = %v/%v/%v", got.Different, got.Extra, got.Same)
	}
}

func TestJSONNeverNull(t *testing.T) {
	var buf bytes.Buffer
	if err := JSON(&buf, diff.Result{KeysOnly: true}, "a", "b"); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, "null") {
		t.Errorf("output contains null:\n%s", out)
	}
	for _, field := range []string{`"missing": []`, `"extra": []`, `"different": []`, `"same": []`} {
		if !strings.Contains(out, field) {
			t.Errorf("output lacks %s:\n%s", field, out)
		}
	}
	if !strings.Contains(out, `"drift": false`) || !strings.Contains(out, `"keys_only": true`) {
		t.Errorf("output flags wrong:\n%s", out)
	}
}
