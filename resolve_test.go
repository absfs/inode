package inode

import (
	"errors"
	filepath "path"
	"strings"
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

	tests := []struct {
		Path string
		Ino  uint64
	}{
		{
			Path: "/",
			Ino:  1,
		},
		{
			Path: "/.",
			Ino:  1,
		},
		{
			Path: "/..",
			Ino:  1,
		},
		{
			Path: "/tmp",
			Ino:  2,
		},
		{
			Path: "/tmp/.",
			Ino:  2,
		},
		{
			Path: "/tmp/..",
			Ino:  1,
		},
		{
			Path: "/tmp/bar",
			Ino:  4,
		},
		{
			Path: "/tmp/bar/.",
			Ino:  4,
		},
		{
			Path: "/tmp/bar/..",
			Ino:  2,
		},
		{
			Path: "/tmp/bat",
			Ino:  5,
		},
		{
			Path: "/tmp/bat/.",
			Ino:  5,
		},
		{
			Path: "/tmp/bat/..",
			Ino:  2,
		},
		{
			Path: "/tmp/foo",
			Ino:  3,
		},
		{
			Path: "/tmp/foo/.",
			Ino:  3,
		},
		{
			Path: "/tmp/foo/..",
			Ino:  2,
		},
	}

	count := 0
	type testcase struct {
		Path string
		Node *Inode
	}

	testoutput := make(chan *testcase)
	var walk func(node *Inode, path string) error
	walk = func(node *Inode, path string) error {
		count++
		if count > 20 {
			return errors.New("counted too far")
		}

		testoutput <- &testcase{path, node}

		if !node.IsDir() {
			if node.Dir.Len() != 0 {
				return errors.New("is directory")
			}
			return nil
		}
		for _, suffix := range []string{"/.", "/.."} {
			if strings.HasSuffix(path, suffix) {
				return nil
			}
		}

		if path == "/" {
			path = ""
		}
		for _, entry := range node.Dir {
			err := walk(entry.Inode, path+"/"+entry.Name)
			if err != nil {
				return err
			}
		}
		return nil
	}

	go func() {
		defer close(testoutput)
		err = walk(root, "/")
		if err != nil {
			t.Error(err)
			return
		}
	}()

	i := 0
	for test := range testoutput {
		if tests[i].Path != test.Path {
			t.Errorf("Path: expected %q, got %q", tests[i].Path, test.Path)
		}

		if tests[i].Ino != test.Node.Ino {
			t.Errorf("Ino: expected %d, got %d -- %q, %q", tests[i].Ino, test.Node.Ino, tests[i].Path, test.Path)
		}
		i++
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

			t.Logf("%d %q \t%q", node.Ino, Path, filepath.Join("/tmp/foo", Path))
			if node.Ino != Ino {
				t.Fatalf("Ino: %d, Expected: %d\n", node.Ino, Ino)
			}
		}
	})
}
