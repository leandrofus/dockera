package compose

import (
	"slices"
	"testing"
)

func TestParseEnv(t *testing.T) {
	got := ParseEnv("# comment\n\nA=1\nexport B = two\nC=\"quoted # not a comment\"\nD='single'\nE=x # trailing\nnot a pair\n=empty\nF=\n")
	want := []string{"A=1", "B=two", "C=quoted # not a comment", "D=single", "E=x", "F="}
	if !slices.Equal(got, want) {
		t.Fatalf("ParseEnv:\n got %q\nwant %q", got, want)
	}
}
