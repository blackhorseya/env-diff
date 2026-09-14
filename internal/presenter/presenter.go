// Package presenter renders a diff.Result for people (terminal) or for
// machines (JSON). Neither output ever contains a variable's value.
package presenter

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
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

// Terminal writes a human-readable report. Only key names are listed. Two
// environments are reported as sections; three or more as a matrix with
// one row per drifted key.
func Terminal(w io.Writer, r diff.Result, color bool) error {
	p := painter(color)
	var b strings.Builder

	title := "Environment Drift"
	if r.Options.KeysOnly {
		title += " (keys only)"
	}
	b.WriteString(p.paint(ansiBold, title) + "\n")

	switch {
	case !r.HasDrift():
		b.WriteString("\n" + p.paint(ansiGreen, "No differences found") + "\n")
	case len(r.Envs) > 2:
		matrix(&b, p, r)
		b.WriteString("\n" + p.paint(ansiBold+ansiRed, plural(r.Count(), "difference")+" found") + "\n")
	default:
		section(&b, p, ansiRed, "Missing in target", r.Missing)
		if !r.Options.AllowExtra {
			section(&b, p, ansiYellow, "Extra in target", r.Extra)
		}
		section(&b, p, ansiCyan, "Different values", r.Different)
		b.WriteString("\n" + p.paint(ansiBold+ansiRed, plural(r.Count(), "difference")+" found") + "\n")
	}
	// Report the ignore count whenever --ignore was given, so a name that
	// matched nothing is visible rather than silently inert.
	if len(r.Options.Ignore) > 0 {
		b.WriteString(plural(len(r.Ignored), "key") + " ignored\n")
	}
	if r.Options.AllowExtra && len(r.Extra) > 0 {
		b.WriteString(plural(len(r.Extra), "extra key") + " allowed\n")
	}

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

// matrix writes one row per drifted key, ordered missing, extra, different,
// with a column per environment. Cells are padded before any color is
// applied so escape codes never disturb the alignment.
func matrix(b *strings.Builder, p painter, r diff.Result) {
	var rows []diff.Row
	for _, status := range []diff.Status{diff.Missing, diff.Extra, diff.Different} {
		if status == diff.Extra && r.Options.AllowExtra {
			continue
		}
		for _, row := range r.Rows {
			if row.Status == status {
				rows = append(rows, row)
			}
		}
	}

	cells := make([][]string, len(rows))
	keyWidth := len("KEY")
	widths := make([]int, len(r.Envs))
	for i, name := range r.Envs {
		widths[i] = len(name)
	}
	for i, row := range rows {
		keyWidth = max(keyWidth, len(row.Key))
		cells[i] = make([]string, len(row.Cells))
		for j, c := range row.Cells {
			cells[i][j] = symbol(row, c)
			widths[j] = max(widths[j], len(cells[i][j]))
		}
	}

	header := pad("KEY", keyWidth)
	for i, name := range r.Envs {
		header += "  " + pad(name, widths[i])
	}
	b.WriteString("\n" + p.paint(ansiBold, strings.TrimRight(header, " ")) + "\n")
	for i, row := range rows {
		line := pad(row.Key, keyWidth)
		for j, cell := range cells[i] {
			line += "  " + pad(cell, widths[j])
		}
		b.WriteString(line + "  " + p.paint(statusColor(row.Status), statusText(row, r.Envs)) + "\n")
	}
}

// symbol renders one cell: "-" when absent, "+" when present, or a group
// letter when the row's present values disagree.
func symbol(row diff.Row, c diff.Cell) string {
	switch {
	case !c.Present:
		return "-"
	case row.Groups() < 2:
		return "+"
	case c.Group < 26:
		return string(rune('a' + c.Group))
	default:
		return strconv.Itoa(c.Group)
	}
}

func statusText(row diff.Row, envs []string) string {
	var names []string
	switch row.Status {
	case diff.Missing:
		for i, c := range row.Cells {
			if !c.Present {
				names = append(names, envs[i])
			}
		}
		return "missing in " + strings.Join(names, ", ")
	case diff.Extra:
		for i, c := range row.Cells {
			if c.Present {
				names = append(names, envs[i])
			}
		}
		return "extra in " + strings.Join(names, ", ")
	}
	return row.Status.String()
}

func statusColor(s diff.Status) string {
	switch s {
	case diff.Missing:
		return ansiRed
	case diff.Extra:
		return ansiYellow
	case diff.Different:
		return ansiCyan
	}
	return ansiGreen
}

func pad(s string, width int) string {
	return s + strings.Repeat(" ", max(0, width-len(s)))
}

func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

type report struct {
	Source     string            `json:"source"`
	Target     string            `json:"target,omitempty"`
	Envs       []string          `json:"envs"`
	KeysOnly   bool              `json:"keys_only"`
	AllowExtra bool              `json:"allow_extra"`
	Drift      bool              `json:"drift"`
	Summary    summary           `json:"summary"`
	Missing    []string          `json:"missing"`
	Extra      []string          `json:"extra"`
	Different  []string          `json:"different"`
	Same       []string          `json:"same"`
	Ignored    []string          `json:"ignored"`
	Matrix     map[string][]*int `json:"matrix"`
}

type summary struct {
	Same      int `json:"same"`
	Missing   int `json:"missing"`
	Extra     int `json:"extra"`
	Different int `json:"different"`
	Ignored   int `json:"ignored"`
}

// JSON writes a machine-readable report. Key lists are always arrays, never
// null. "matrix" maps every key to one entry per environment: the value
// group number, or null where the key is absent. "target" is present only
// when exactly two environments were compared. With allow_extra, "extra"
// still lists the keys but "drift" ignores them.
func JSON(w io.Writer, r diff.Result) error {
	rep := report{
		Envs:       orEmpty(r.Envs),
		KeysOnly:   r.Options.KeysOnly,
		AllowExtra: r.Options.AllowExtra,
		Drift:      r.HasDrift(),
		Summary: summary{
			Same:      len(r.Same),
			Missing:   len(r.Missing),
			Extra:     len(r.Extra),
			Different: len(r.Different),
			Ignored:   len(r.Ignored),
		},
		Missing:   orEmpty(r.Missing),
		Extra:     orEmpty(r.Extra),
		Different: orEmpty(r.Different),
		Same:      orEmpty(r.Same),
		Ignored:   orEmpty(r.Ignored),
		Matrix:    map[string][]*int{},
	}
	if len(r.Envs) > 0 {
		rep.Source = r.Envs[0]
	}
	if len(r.Envs) == 2 {
		rep.Target = r.Envs[1]
	}
	for _, row := range r.Rows {
		groups := make([]*int, len(row.Cells))
		for i, c := range row.Cells {
			if c.Present {
				groups[i] = new(c.Group)
			}
		}
		rep.Matrix[row.Key] = groups
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(rep)
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
