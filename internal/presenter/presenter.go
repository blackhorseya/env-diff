// Package presenter renders a diff.Result for people (terminal) or for
// machines (JSON). Neither output ever contains a variable's value.
package presenter

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/blackhorseya/env-diff/internal/diff"
)

const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiRed    = "\x1b[31m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiCyan   = "\x1b[36m"
)

// painter wraps text in ANSI codes when enabled and is a no-op otherwise,
// so the layout is identical with and without color.
type painter bool

func (p painter) paint(code, s string) string {
	if !p {
		return s
	}
	return code + s + ansiReset
}

// Terminal writes a human-readable report. Only key names are listed.
func Terminal(w io.Writer, r diff.Result, color bool) error {
	p := painter(color)
	var b strings.Builder

	title := "Environment Drift"
	if r.KeysOnly {
		title += " (keys only)"
	}
	b.WriteString(p.paint(ansiBold, title) + "\n")

	if !r.HasDrift() {
		b.WriteString("\n" + p.paint(ansiGreen, "No differences found") + "\n")
		_, err := io.WriteString(w, b.String())
		return err
	}

	section(&b, p, ansiRed, "Missing in target", r.Missing)
	section(&b, p, ansiYellow, "Extra in target", r.Extra)
	section(&b, p, ansiCyan, "Different values", r.Different)
	b.WriteString("\n" + p.paint(ansiBold+ansiRed, plural(r.Count(), "difference")+" found") + "\n")

	_, err := io.WriteString(w, b.String())
	return err
}

func section(b *strings.Builder, p painter, code, title string, keys []string) {
	if len(keys) == 0 {
		return
	}
	b.WriteString("\n" + p.paint(code, title) + "\n")
	for _, key := range keys {
		b.WriteString("  " + key + "\n")
	}
}

func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

type report struct {
	Source    string   `json:"source"`
	Target    string   `json:"target"`
	KeysOnly  bool     `json:"keys_only"`
	Drift     bool     `json:"drift"`
	Summary   summary  `json:"summary"`
	Missing   []string `json:"missing"`
	Extra     []string `json:"extra"`
	Different []string `json:"different"`
	Same      []string `json:"same"`
}

type summary struct {
	Same      int `json:"same"`
	Missing   int `json:"missing"`
	Extra     int `json:"extra"`
	Different int `json:"different"`
}

// JSON writes a machine-readable report. Key lists are always arrays,
// never null, so consumers can index them without a nil check.
func JSON(w io.Writer, r diff.Result, source, target string) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(report{
		Source:   source,
		Target:   target,
		KeysOnly: r.KeysOnly,
		Drift:    r.HasDrift(),
		Summary: summary{
			Same:      len(r.Same),
			Missing:   len(r.Missing),
			Extra:     len(r.Extra),
			Different: len(r.Different),
		},
		Missing:   orEmpty(r.Missing),
		Extra:     orEmpty(r.Extra),
		Different: orEmpty(r.Different),
		Same:      orEmpty(r.Same),
	})
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
