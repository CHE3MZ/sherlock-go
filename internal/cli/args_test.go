package cli

import "testing"

func TestReorderArgs(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string // flags first (in order), then positionals
	}{
		{"interspersed flag after positional",
			[]string{"torvalds", "--site", "GitHub"},
			[]string{"--site", "GitHub", "torvalds"}},
		{"negative numeric value consumed",
			[]string{"--timeout", "-5", "user"},
			[]string{"--timeout", "-5", "user"}},
		{"combined short booleans expand",
			[]string{"-vd", "user"},
			[]string{"-v", "-d", "user"}},
		{"equals form stays intact",
			[]string{"--json=foo.json", "u"},
			[]string{"--json=foo.json", "u"}},
		{"value short with scheme",
			[]string{"user", "-p", "socks5://h:1080"},
			[]string{"-p", "socks5://h:1080", "user"}},
		{"positionals only",
			[]string{"u1", "u2"},
			[]string{"u1", "u2"}},
		{"trailing valueless flag kept for parser error",
			[]string{"--site"},
			[]string{"--site"}},
		{"multi flags and multi positionals",
			[]string{"u1", "--csv", "u2", "--site", "X", "--txt"},
			[]string{"--csv", "--site", "X", "--txt", "u1", "u2"}},
		{"long bool not split",
			[]string{"--print-all", "u"},
			[]string{"--print-all", "u"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ReorderArgs(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v want %v", got, tc.want)
				}
			}
		})
	}
}
