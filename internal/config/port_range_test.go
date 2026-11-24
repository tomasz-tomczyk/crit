package config

import (
	"encoding/json"
	"flag"
	"testing"
)

func TestParsePortRange(t *testing.T) {
	tests := []struct {
		in      string
		want    PortRange
		wantErr bool
	}{
		{in: "", want: PortRange{}},
		{in: "0", want: PortRange{}},
		{in: "47800", want: PortRange{First: 47800, Last: 47800}},
		{in: " 47800 ", want: PortRange{First: 47800, Last: 47800}},
		{in: "47800-47819", want: PortRange{First: 47800, Last: 47819}},
		{in: "47800-47800", want: PortRange{First: 47800, Last: 47800}},
		{in: "1-65535", want: PortRange{First: 1, Last: 65535}},
		{in: "47819-47800", wantErr: true},
		{in: "0-10", wantErr: true},
		{in: "65536", wantErr: true},
		{in: "1-65536", wantErr: true},
		{in: "-1", wantErr: true},
		{in: "abc", wantErr: true},
		{in: "47800-", wantErr: true},
		{in: "-47800", wantErr: true},
		{in: "1-2-3", wantErr: true},
		{in: "+80", wantErr: true},
		{in: "47800-+47819", wantErr: true},
		{in: "47800 - 47819", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParsePortRange(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParsePortRange(%q) = %+v, want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParsePortRange(%q) error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Fatalf("ParsePortRange(%q) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}

func TestPortRangeStringRoundTrip(t *testing.T) {
	for _, r := range []PortRange{{}, SinglePort(8080), {First: 47800, Last: 47819}} {
		got, err := ParsePortRange(r.String())
		if err != nil {
			t.Fatalf("ParsePortRange(%q): %v", r.String(), err)
		}
		if got != r {
			t.Fatalf("round trip of %+v via %q = %+v", r, r.String(), got)
		}
	}
	if got := (PortRange{First: 47800, Last: 47819}).String(); got != "47800-47819" {
		t.Fatalf("String() = %q, want 47800-47819", got)
	}
	if got := SinglePort(8080).String(); got != "8080" {
		t.Fatalf("String() = %q, want 8080", got)
	}
}

func TestPortRangeUnmarshalJSON(t *testing.T) {
	tests := []struct {
		in      string
		want    PortRange
		wantErr bool
	}{
		{in: `{"port": 3456}`, want: SinglePort(3456)},
		{in: `{"port": "3456"}`, want: SinglePort(3456)},
		{in: `{"port": "47800-47819"}`, want: PortRange{First: 47800, Last: 47819}},
		{in: `{"port": 0}`, want: PortRange{}},
		{in: `{"port": null}`, want: PortRange{}},
		{in: `{}`, want: PortRange{}},
		{in: `{"port": "47819-47800"}`, wantErr: true},
		{in: `{"port": 70000}`, wantErr: true},
		{in: `{"port": -1}`, wantErr: true},
		{in: `{"port": 1.5}`, wantErr: true},
		{in: `{"port": true}`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			var cfg Config
			err := json.Unmarshal([]byte(tt.in), &cfg)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Unmarshal(%s) = %+v, want error", tt.in, cfg.Port)
				}
				return
			}
			if err != nil {
				t.Fatalf("Unmarshal(%s): %v", tt.in, err)
			}
			if cfg.Port != tt.want {
				t.Fatalf("Unmarshal(%s) port = %+v, want %+v", tt.in, cfg.Port, tt.want)
			}
		})
	}
}

func TestPortRangeMarshalJSON(t *testing.T) {
	tests := []struct {
		r    PortRange
		want string
	}{
		{r: PortRange{}, want: `0`},
		{r: SinglePort(3456), want: `3456`},
		{r: PortRange{First: 47800, Last: 47819}, want: `"47800-47819"`},
	}
	for _, tt := range tests {
		data, err := json.Marshal(tt.r)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != tt.want {
			t.Fatalf("Marshal(%+v) = %s, want %s", tt.r, data, tt.want)
		}
		var back PortRange
		if err := json.Unmarshal(data, &back); err != nil {
			t.Fatal(err)
		}
		if back != tt.r {
			t.Fatalf("round trip of %+v = %+v", tt.r, back)
		}
	}

	data, err := json.Marshal(Config{})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "{}" {
		t.Fatalf("Marshal(Config{}) = %s, want {} (unset port omitted)", data)
	}
}

func TestPortRangeFlagValue(t *testing.T) {
	var r PortRange
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(discard{})
	fs.Var(&r, "port", "")
	if err := fs.Parse([]string{"--port", "47800-47819"}); err != nil {
		t.Fatal(err)
	}
	if r != (PortRange{First: 47800, Last: 47819}) {
		t.Fatalf("flag value = %+v", r)
	}

	var bad PortRange
	fs = flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(discard{})
	fs.Var(&bad, "port", "")
	if err := fs.Parse([]string{"--port", "nope"}); err == nil {
		t.Fatal("expected parse error for --port nope")
	}
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
