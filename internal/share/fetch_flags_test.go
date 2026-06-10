package share

import "testing"

func TestParseFetchOutputDir(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"no args", nil, ""},
		{"empty slice", []string{}, ""},
		{"--output long flag", []string{"--output", "/tmp/x"}, "/tmp/x"},
		{"-o short flag", []string{"-o", "out"}, "out"},
		{"last value wins", []string{"--output", "first", "-o", "second"}, "second"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseFetchOutputDir(c.args)
			if err != nil {
				t.Fatalf("parseFetchOutputDir(%v): %v", c.args, err)
			}
			if got != c.want {
				t.Errorf("parseFetchOutputDir(%v) = %q, want %q", c.args, got, c.want)
			}
		})
	}
}
