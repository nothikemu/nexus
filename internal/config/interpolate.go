package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

var refRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(:-([^}]*))?\}`)

// MissingVarError reports environment variables a value needs but which are unset.
type MissingVarError struct {
	Field string
	Vars  []string
}

func (e *MissingVarError) Error() string {
	return fmt.Sprintf("%s references unset environment variable %s", e.Field, strings.Join(e.Vars, ", "))
}

// Interpolate expands ${VAR} and ${VAR:-default} references using lookup.
// Unset variables without a default are reported, never silently emptied —
// a missing secret must fail loudly rather than connect somewhere unexpected.
func Interpolate(field, value string, lookup func(string) (string, bool)) (string, error) {
	if lookup == nil {
		lookup = os.LookupEnv
	}
	var missing []string
	out := refRe.ReplaceAllStringFunc(value, func(ref string) string {
		m := refRe.FindStringSubmatch(ref)
		if v, ok := lookup(m[1]); ok && v != "" {
			return v
		}
		if m[2] != "" {
			return m[3]
		}
		missing = append(missing, m[1])
		return ""
	})
	if len(missing) > 0 {
		return "", &MissingVarError{Field: field, Vars: missing}
	}
	return out, nil
}

// References lists the environment variables a value refers to.
func References(value string) []string {
	var out []string
	for _, m := range refRe.FindAllStringSubmatch(value, -1) {
		out = append(out, m[1])
	}
	return out
}
