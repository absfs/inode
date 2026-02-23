package inode

import (
	"path"
	"testing"
)

func TestResolve(t *testing.T) {
	ino := new(Ino)

	var root, parent, dir *Inode
	root = ino.NewDir(0777)
	parent = root

	dir = ino.NewDir(0777)
	err := parent.Link("tmp", dir)
	if err != nil {
		t.Fatal(err)
	}
	err = dir.Link("..", parent)
	if err != nil {
		t.Fatal(err)
	}

	parent = dir
	dir = ino.NewDir(0777)
	parent.Link("foo", dir)
	err = dir.Link("..", parent)
	if err != nil {
		t.Fatal(err)
	}

	dir = ino.NewDir(0777)
	parent.Link("bar", dir)
	err = dir.Link("..", parent)
	if err != nil {
		t.Fatal(err)
	}

	dir = ino.NewDir(0777)
	parent.Link("bat", dir)
	err = dir.Link("..", parent)
	if err != nil {
		t.Fatal(err)
	}

	// Walk the tree and verify all expected nodes are visited
	expected := map[string]uint64{
		"/":           1,
		"/.":          1,
		"/..":         1,
		"/tmp":        2,
		"/tmp/.":      2,
		"/tmp/..":     1,
		"/tmp/bar":    4,
		"/tmp/bar/.":  4,
		"/tmp/bar/..": 2,
		"/tmp/bat":    5,
		"/tmp/bat/.":  5,
		"/tmp/bat/..": 2,
		"/tmp/foo":    3,
		"/tmp/foo/.":  3,
		"/tmp/foo/..": 2,
	}

	got := make(map[string]uint64)
	err = Walk(root, "/", func(p string, n *Inode) error {
		got[p] = n.Ino
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != len(expected) {
		t.Fatalf("walk visited %d nodes, expected %d", len(got), len(expected))
	}
	for p, ino := range expected {
		if got[p] != ino {
			t.Errorf("path %q: got ino %d, expected %d", p, got[p], ino)
		}
	}

	t.Run("resolve", func(t *testing.T) {
		tests := make(map[string]uint64)
		tests["/"] = 1
		tests["/tmp"] = 2
		tests["/tmp/bar"] = 4
		tests["/tmp/bat"] = 5
		tests["/tmp/foo"] = 3
		var dir *Inode
		for Path, Ino := range tests {
			node, err := root.Resolve(Path)
			if err != nil {
				t.Fatal(err)
			}
			if Path == "/tmp/foo" {
				dir = node
			}
			if node.Ino != Ino {
				t.Fatalf("Ino: %d, Expected: %d\n", node.Ino, Ino)
			}
		}

		// test relative paths
		tests = make(map[string]uint64)
		tests["../.."] = 1
		tests[".."] = 2
		tests["../bar"] = 4
		tests["../bat"] = 5
		tests["."] = 3
		for Path, Ino := range tests {
			node, err := dir.Resolve(Path)
			if err != nil {
				t.Fatal(err)
			}

			t.Logf("%d %q \t%q", node.Ino, Path, path.Join("/tmp/foo", Path))
			if node.Ino != Ino {
				t.Fatalf("Ino: %d, Expected: %d\n", node.Ino, Ino)
			}
		}
	})
}
