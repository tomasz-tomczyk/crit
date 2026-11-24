package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// PortRange is a requested listen port: a single port ("47800") or an
// inclusive range ("47800-47819"). The zero value means any free port.
// First == Last is a single port.
type PortRange struct {
	First int
	Last  int
}

// SinglePort returns a PortRange for one port. SinglePort(0) is the zero value.
func SinglePort(port int) PortRange {
	return PortRange{First: port, Last: port}
}

// ParsePortRange parses "", "0", "N" or "A-B". Ports must be 1-65535 and
// A must not be greater than B.
func ParsePortRange(s string) (PortRange, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" {
		return PortRange{}, nil
	}
	firstStr, lastStr, isRange := strings.Cut(s, "-")
	first, err := parsePort(firstStr)
	if err != nil {
		return PortRange{}, fmt.Errorf("invalid port %q: %w", s, err)
	}
	if !isRange {
		return SinglePort(first), nil
	}
	last, err := parsePort(lastStr)
	if err != nil {
		return PortRange{}, fmt.Errorf("invalid port range %q: %w", s, err)
	}
	if first > last {
		return PortRange{}, fmt.Errorf("invalid port range %q: first port is greater than last", s)
	}
	return PortRange{First: first, Last: last}, nil
}

func parsePort(s string) (int, error) {
	p, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("not a number")
	}
	if p < 1 || p > 65535 {
		return 0, fmt.Errorf("port must be 1-65535")
	}
	return p, nil
}

// IsZero reports whether no port was requested (any free port).
func (r PortRange) IsZero() bool {
	return r == PortRange{}
}

// IsRange reports whether r covers more than one port.
func (r PortRange) IsRange() bool {
	return r.First != r.Last
}

// String returns "" for the zero value, "N" for one port and "A-B" for a
// range. ParsePortRange reads it back.
func (r PortRange) String() string {
	switch {
	case r.IsZero():
		return ""
	case r.IsRange():
		return fmt.Sprintf("%d-%d", r.First, r.Last)
	default:
		return strconv.Itoa(r.First)
	}
}

// Set implements flag.Value.
func (r *PortRange) Set(s string) error {
	parsed, err := ParsePortRange(s)
	if err != nil {
		return err
	}
	*r = parsed
	return nil
}

// UnmarshalJSON accepts a number (3456) or a string ("3456", "47800-47819").
func (r *PortRange) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("null")) {
		*r = PortRange{}
		return nil
	}
	if len(data) > 0 && data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		return r.Set(s)
	}
	var n int
	if err := json.Unmarshal(data, &n); err != nil {
		return fmt.Errorf("port must be a number or a \"first-last\" string: %w", err)
	}
	if n == 0 {
		*r = PortRange{}
		return nil
	}
	if n < 1 || n > 65535 {
		return fmt.Errorf("invalid port %d: port must be 1-65535", n)
	}
	*r = SinglePort(n)
	return nil
}

// MarshalJSON writes a number for a single port (0 when unset) and a
// "first-last" string for a range.
func (r PortRange) MarshalJSON() ([]byte, error) {
	if r.IsRange() {
		return json.Marshal(r.String())
	}
	return json.Marshal(r.First)
}
