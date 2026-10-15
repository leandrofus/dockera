package auth

import "testing"

func TestInListWindows(t *testing.T) {
	old := windowsNames
	windowsNames = true
	defer func() { windowsNames = old }()
	cases := []struct {
		list []string
		name string
		want bool
	}{
		{[]string{"Administradores"}, "administradores", true},
		{[]string{"bob"}, `CORP\bob`, true},      // bare entry: any domain
		{[]string{"bob"}, `.\Bob`, true},         // this computer
		{[]string{`CORP\bob`}, `corp\BOB`, true}, // qualified entry: case only
		{[]string{`CORP\bob`}, `OTHER\bob`, false},
		{[]string{`CORP\bob`}, "bob", false},
		{[]string{"bob@corp.example"}, `CORP\bob`, false},
		{[]string{"alice"}, "bob", false},
	}
	for _, c := range cases {
		if got := inList(c.list, c.name); got != c.want {
			t.Errorf("inList(%q, %q) = %v, want %v", c.list, c.name, got, c.want)
		}
	}
}

func TestInListPOSIX(t *testing.T) {
	old := windowsNames
	windowsNames = false
	defer func() { windowsNames = old }()
	if inList([]string{"Bob"}, "bob") {
		t.Error("POSIX names are case-sensitive")
	}
	if !inList([]string{"bob"}, "bob") {
		t.Error("exact name must match")
	}
}
