package workspace

import (
	"strings"
	"testing"
)

// newTestTerm returns a 10×3 emulator that has been sent out.
func newTestTerm(t *testing.T, out string) *vterm {
	t.Helper()
	term := newVterm(10, 3, 100, nil)
	if _, err := term.Write([]byte(strings.ReplaceAll(out, "\n", "\r\n"))); err != nil {
		t.Fatal(err)
	}
	return term
}

func TestTermText(t *testing.T) {
	tests := []struct {
		name     string
		out      string
		scroll   int
		from, to pos
		want     string
	}{
		{
			name: "within one row",
			out:  "hello world",
			from: pos{1, 0}, to: pos{3, 0},
			want: "ell",
		},
		{
			name: "rows run to the edge between the first and last",
			out:  "ab  \ncd\nef",
			from: pos{1, 0}, to: pos{0, 2},
			want: "b\ncd\ne",
		},
		{
			name: "trailing blanks are trimmed on every row",
			out:  "a   \nb",
			from: pos{0, 0}, to: pos{9, 1},
			want: "a\nb",
		},
		{
			name: "a blank selection is empty",
			out:  "a",
			from: pos{3, 0}, to: pos{8, 0},
			want: "",
		},
		{
			name:   "scrolled back, rows come from history then the screen",
			out:    "one\ntwo\nthree\nfour\nfive",
			scroll: 2,
			from:   pos{1, 0}, to: pos{1, 2},
			want: "ne\ntwo\nth",
		},
		{
			name: "a wide character is copied once",
			out:  "a界b",
			from: pos{0, 0}, to: pos{3, 0},
			want: "a界b",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			term := newTestTerm(t, tc.out)
			if got := term.Text(tc.scroll, tc.from, tc.to); got != tc.want {
				t.Errorf("Text = %q, want %q", got, tc.want)
			}
		})
	}
}
