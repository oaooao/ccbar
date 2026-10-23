package main

import "testing"

func TestParseGitStatus(t *testing.T) {
	cases := []struct {
		name string
		out  string
		want GitInfo
	}{
		{"clean branch", "# branch.oid 3bbccf5afcc9421fdd45502da0153f9f52d4cdee\n# branch.head master\n# branch.upstream origin/master\n# branch.ab +1 -0\n",
			GitInfo{Branch: "master"}},
		{"staged, modified, both, renamed, conflict",
			"# branch.oid abc1234def\n# branch.head main\n" +
				"1 M. N... 100644 100644 100644 a b staged.go\n" +
				"1 .M N... 100644 100644 100644 a b modified.go\n" +
				"1 MM N... 100644 100644 100644 a b both.go\n" +
				"2 R. N... 100644 100644 100644 a b R100 new.go\told.go\n" +
				"u UU N... 100644 100644 100644 100644 a b c conflict.go\n",
			GitInfo{Branch: "main", Staged: 3, Modified: 3}},
		{"detached", "# branch.oid 0123456789abcdef\n# branch.head (detached)\n",
			GitInfo{Branch: "0123456"}},
		{"no commits yet", "# branch.oid (initial)\n# branch.head main\n",
			GitInfo{Branch: "main"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parseGitStatus(c.out); *got != c.want {
				t.Fatalf("got %+v, want %+v", *got, c.want)
			}
		})
	}
}
