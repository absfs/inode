package inode

import (
	"fmt"
	"os"
	"path"
	"strings"
	"testing"
)

func TestInode(t *testing.T) {
	var ino Ino
	root := ino.NewDir(0777)
	children := make([]*Inode, 100)
	for i := range children {
		ino++
		children[i] = ino.New(0666)
	}

	NlinkTest := func(location string, count int) {
		for _, n := range children {
			if n.Nlink != uint64(count) {
				t.Fatalf("%s: incorrect link count %d != %d", location, n.Nlink, count)
			}
		}
	}
	NlinkTest("NLT 1", 0)

	paths := make(map[string]*Inode)
	paths["/"] = root

	for i, n := range children {
		name := fmt.Sprintf("file.%04d.txt", i+2)

		err := root.Link(name, n)
		name = path.Join("/", name)
		paths[name] = n
		if err != nil {
			t.Fatal(err)
		}
	}

	NlinkTest("NLT 2", 1)

	CWD := "/"
	cwd := &CWD
	Mkdir := func(p string, perm os.FileMode) error {

		if !path.IsAbs(p) {
			p = path.Join(*cwd, p)
		}

		// does this path already exist?
		_, ok := paths[p]
		if ok { // if so, error
			return os.ErrExist
		}

		// find the parent directory
		dir, name := path.Split(p)
		dir = path.Clean(dir)
		parent, ok := paths[dir]
		if !ok {
			return os.ErrNotExist
		}

		// build the node
		dirnode := ino.NewDir(0777)
		dirnode.Link("..", parent)
		// add a link to the parent directory
		parent.Link(name, dirnode)

		paths[p] = dirnode

		if dirnode.Nlink != 2 {
			return fmt.Errorf("incorrect link count for %q", p)
		}
		return nil // done?
	}

	err := Mkdir("dir0001", 0777)
	if err != nil {
		t.Fatal(err)
	}

	CWD = "/dir0001"
	err = Mkdir("dir0002", 0777)
	if err != nil {
		t.Fatal(err)
	}

	dirnode, ok := paths["/dir0001/dir0002"]
	if !ok {
		t.Fatal("broken path")
	}

	// dirnode.link(name, child)
	for p, n := range paths {
		name := path.Base(p)
		if !strings.HasPrefix(name, "file") {
			continue
		}
		dirnode.Link(name, n)
		name = path.Join("/dir0001/dir0002", name)
		paths[name] = n
	}

	NlinkTest("NLT 3", 2)

	for p := range paths {
		if !strings.HasPrefix(p, "/file") {
			continue
		}

		name := path.Base(p)
		err := root.Unlink(name)
		if err != nil {
			t.Fatalf("%s %s", name, err)
		}
		delete(paths, p)
	}

	NlinkTest("NLT 4", 1)

	// Walk the tree and collect path→ino mappings
	expected := map[string]uint64{
		"/":         1,
		"/.":        1,
		"/..":       1,
		"/dir0001":  202,
		"/dir0001/.":  202,
		"/dir0001/..": 1,
		"/dir0001/dir0002":    203,
		"/dir0001/dir0002/.":  203,
		"/dir0001/dir0002/..": 202,
	}
	for i := 0; i < 100; i++ {
		name := fmt.Sprintf("/dir0001/dir0002/file.%04d.txt", i+2)
		expected[name] = uint64(i*2 + 3)
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
			t.Fatalf("path %q: got ino %d, expected %d", p, got[p], ino)
		}
	}
}
