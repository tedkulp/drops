package model

import (
	"fmt"
	"strings"
	"time"
)

// Timestamp is RFC3339Nano text in UTC. Parsed values retain their source bytes
// so importing and exporting does not silently reformat historical timestamps.
type Timestamp string

func NewTimestamp(value time.Time) Timestamp {
	return Timestamp(value.UTC().Format(time.RFC3339Nano))
}

func ParseTimestamp(raw string) (Timestamp, error) {
	if raw == "" || !strings.HasSuffix(raw, "Z") {
		return "", fmt.Errorf("%w: timestamp must be RFC3339Nano UTC text", ErrInvalid)
	}
	if fraction := strings.IndexByte(raw, '.'); fraction >= 0 {
		if digits := len(raw) - fraction - 2; digits < 1 || digits > 9 {
			return "", fmt.Errorf("%w: timestamp fractional seconds must have 1 to 9 digits", ErrInvalid)
		}
	}
	if strings.ContainsRune(raw, ',') {
		return "", fmt.Errorf("%w: timestamp must use a decimal point", ErrInvalid)
	}
	if _, err := time.Parse(time.RFC3339Nano, raw); err != nil {
		return "", fmt.Errorf("%w: timestamp %q: %v", ErrInvalid, raw, err)
	}
	return Timestamp(raw), nil
}

func (stamp Timestamp) Validate() error {
	_, err := ParseTimestamp(string(stamp))
	return err
}

func (stamp Timestamp) Time() (time.Time, error) {
	if err := stamp.Validate(); err != nil {
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339Nano, string(stamp))
}
