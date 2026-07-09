package store

import (
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

// renderValue converts a decoded pgx value into a display string.
func renderValue(v any) string {
	switch x := v.(type) {
	case nil:
		return "NULL"
	case string:
		return x
	case []byte:
		return fmt.Sprintf("\\x%x", x)
	case time.Time:
		return x.Format(time.RFC3339)
	case fmt.Stringer:
		return x.String()
	default:
		return fmt.Sprintf("%v", x)
	}
}
