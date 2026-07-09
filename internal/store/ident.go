package store

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// validateIdent rejects identifiers that are empty, too long, or contain
// characters outside the safe set. This is a defence-in-depth check layered
// on top of quoteIdent for identifiers that come from user input.
func validateIdent(name string) error {
	if name == "" {
		return fmt.Errorf("identifier must not be empty")
	}
	if len(name) > 63 {
		return fmt.Errorf("identifier %q exceeds 63 characters", name)
	}
	for _, r := range name {
		ok := r == '_' ||
			(r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9')
		if !ok {
			return fmt.Errorf("identifier %q contains an unsupported character %q", name, string(r))
		}
	}
	return nil
}

// quoteIdent double-quotes a PostgreSQL identifier, escaping embedded quotes.
func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// renderValue converts a decoded pgx value into a display string. pgx decodes
// many PostgreSQL types (numeric, uuid, json, arrays, ...) into pgtype structs;
// most implement driver.Valuer, which yields a clean textual form.
func renderValue(v any) string {
	switch x := v.(type) {
	case nil:
		return "NULL"
	case string:
		return x
	case []byte:
		if json.Valid(x) {
			return string(x)
		}
		return fmt.Sprintf("\\x%x", x)
	case time.Time:
		return x.Format(time.RFC3339)
	case fmt.Stringer:
		return x.String()
	case driver.Valuer:
		val, err := x.Value()
		if err != nil || val == nil {
			return "NULL"
		}
		// Value() returns a primitive (string/int64/float64/bool/time/[]byte);
		// recurse once to format it consistently.
		if _, isValuer := val.(driver.Valuer); isValuer {
			return fmt.Sprintf("%v", val) // guard against pathological recursion
		}
		return renderValue(val)
	default:
		// pgx may return maps/slices for composite/array/json types; JSON-encode
		// them for a readable, structured representation.
		if b, err := json.Marshal(x); err == nil {
			return string(b)
		}
		return fmt.Sprintf("%v", x)
	}
}
