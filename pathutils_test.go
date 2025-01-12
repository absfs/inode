package inode

import (
	"testing"
)

func TestPopPath(t *testing.T) {
	tests := []struct {
		Input string
		Name  string
		Trim  string
	}{
		{"", "", ""},
		{"/", "/", ""},
		{"/foo/bar/bat", "/", "foo/bar/bat"},
		{"foo/bar/bat", "foo", "bar/bat"},
		{"bar/bat", "bar", "bat"},
		{"bat", "bat", ""},
	}

	for i, test := range tests {
		name, trim := PopPath(test.Input)
		t.Logf("%q, %q := popPath(%q)", name, trim, test.Input)
		if name != test.Name {
			t.Fatalf("%d: %s != %s", i, name, test.Name)
		}
		if trim != test.Trim {
			t.Fatalf("%d: %s != %s", i, trim, test.Trim)
		}
	}
}
