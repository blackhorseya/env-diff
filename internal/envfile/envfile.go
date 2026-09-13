// Package envfile parses dotenv-style files into a key-value collection.
//
// The parser is deliberately strict and predictable rather than shell
// compatible. Supported syntax:
//
//	KEY=value            unquoted value, surrounding whitespace trimmed
//	KEY="value"          double-quoted; \" and \\ are unescaped, nothing else
//	KEY='value'          single-quoted; taken literally
//	KEY=                 empty value
//	export KEY=value     optional export prefix
//	# comment            full-line comment
//	KEY=value # comment  inline comment, only when "#" follows whitespace
//
// Keys must match [A-Za-z_][A-Za-z0-9_]* and are case-sensitive. A UTF-8
// byte order mark and CRLF line endings are tolerated. Not supported:
// multi-line quoted values (error), variable expansion (${VAR} is kept as
// literal text), and duplicate keys (error, so ambiguous configuration is
// never silently resolved).
package envfile

import (
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strings"
)

// Env is a parsed environment file.
//
// Values are unexported and never included in String or GoString, so an Env
// can be passed to fmt or a logger without exposing secrets.
type Env struct {
	vars map[string]string
}

// Keys returns the variable names in sorted order.
func (x Env) Keys() []string {
	return slices.Sorted(maps.Keys(x.vars))
}

// Lookup returns the value for key and whether the key exists.
func (x Env) Lookup(key string) (string, bool) {
	v, ok := x.vars[key]
	return v, ok
}

// Len returns the number of variables.
func (x Env) Len() int {
	return len(x.vars)
}

// String describes the Env without revealing any value.
func (x Env) String() string {
	return fmt.Sprintf("envfile.Env{%d keys}", len(x.vars))
}

// GoString describes the Env without revealing any value (used by %#v).
func (x Env) GoString() string {
	return x.String()
}

// Error is a syntax error at a specific line. It never carries the content
// of the offending line, so it is safe to print.
type Error struct {
	Line int
	Msg  string
}

func (e *Error) Error() string {
	return fmt.Sprintf("line %d: %s", e.Line, e.Msg)
}

// ParseFile reads and parses the file at path.
func ParseFile(path string) (Env, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Env{}, err
	}
	env, err := parse(string(data))
	if err != nil {
		return Env{}, fmt.Errorf("%s: %w", path, err)
	}
	return env, nil
}

// Parse parses dotenv content from r.
func Parse(r io.Reader) (Env, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return Env{}, err
	}
	return parse(string(data))
}

func parse(text string) (Env, error) {
	vars := map[string]string{}
	definedAt := map[string]int{}
	text = strings.TrimPrefix(text, "\uFEFF")

	n := 0
	for raw := range strings.Lines(text) {
		n++
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, err := parseAssignment(line)
		if err != nil {
			return Env{}, &Error{Line: n, Msg: err.Error()}
		}
		if first, dup := definedAt[key]; dup {
			return Env{}, &Error{
				Line: n,
				Msg:  fmt.Sprintf("duplicate key %q (already defined at line %d)", key, first),
			}
		}
		definedAt[key] = n
		vars[key] = value
	}
	return Env{vars: vars}, nil
}

// parseAssignment splits one non-blank, non-comment line into key and value.
// Error messages must not include any part of the line.
func parseAssignment(line string) (key, value string, err error) {
	if rest, ok := strings.CutPrefix(line, "export"); ok && rest != "" && isBlank(rest[0]) {
		line = strings.TrimLeft(rest, " \t")
	}
	key, rawValue, found := strings.Cut(line, "=")
	if !found {
		return "", "", errors.New("expected KEY=VALUE")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return "", "", errors.New("missing variable name before \"=\"")
	}
	if !validKey(key) {
		return "", "", errors.New("invalid variable name")
	}
	value, err = parseValue(rawValue)
	if err != nil {
		return "", "", err
	}
	return key, value, nil
}

func parseValue(raw string) (string, error) {
	s := strings.TrimLeft(raw, " \t")
	if s == "" {
		return "", nil
	}
	switch s[0] {
	case '"':
		return parseDoubleQuoted(s)
	case '\'':
		return parseSingleQuoted(s)
	case '#':
		if len(s) < len(raw) {
			// Whitespace before "#": the whole value is a comment.
			return "", nil
		}
	}
	// Unquoted. Whitespace followed by "#" starts a comment; a "#" glued to
	// the value (PASSWORD=abc#123) is part of it.
	if i := inlineCommentIndex(s); i >= 0 {
		s = s[:i]
	}
	return strings.TrimRight(s, " \t"), nil
}

func parseDoubleQuoted(s string) (string, error) {
	var b strings.Builder
	for i := 1; i < len(s); i++ {
		switch c := s[i]; c {
		case '\\':
			if i+1 < len(s) && (s[i+1] == '"' || s[i+1] == '\\') {
				b.WriteByte(s[i+1])
				i++
				continue
			}
			b.WriteByte(c)
		case '"':
			return b.String(), checkAfterQuote(s[i+1:])
		default:
			b.WriteByte(c)
		}
	}
	return "", errors.New("unterminated double-quoted value")
}

func parseSingleQuoted(s string) (string, error) {
	end := strings.IndexByte(s[1:], '\'')
	if end < 0 {
		return "", errors.New("unterminated single-quoted value")
	}
	return s[1 : 1+end], checkAfterQuote(s[2+end:])
}

// checkAfterQuote allows only whitespace and an optional comment after a
// closing quote.
func checkAfterQuote(rest string) error {
	rest = strings.TrimLeft(rest, " \t")
	if rest != "" && rest[0] != '#' {
		return errors.New("unexpected text after closing quote")
	}
	return nil
}

func inlineCommentIndex(s string) int {
	for i := 1; i < len(s); i++ {
		if s[i] == '#' && isBlank(s[i-1]) {
			return i
		}
	}
	return -1
}

func isBlank(c byte) bool {
	return c == ' ' || c == '\t'
}

func validKey(key string) bool {
	for i := range len(key) {
		c := key[i]
		switch {
		case c == '_', 'A' <= c && c <= 'Z', 'a' <= c && c <= 'z':
		case '0' <= c && c <= '9' && i > 0:
		default:
			return false
		}
	}
	return key != ""
}
