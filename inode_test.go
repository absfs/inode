package inode

import (
	"errors"
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

	type testcase struct {
		Path string
		Node *Inode
	}
	testoutput := make(chan *testcase)
	var walk func(node *Inode, path string) error
	walk = func(node *Inode, path string) error {
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
		for _, entry := range node.ReadDir() {
			err := walk(entry.Inode, path+"/"+entry.Name())
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

	tests := []struct {
		Path string
		Ino  uint64
	}{
		{"/", 1},
		{"/.", 1},
		{"/..", 1},
		{"/dir0001", 202},
		{"/dir0001/.", 202},
		{"/dir0001/..", 1},
		{"/dir0001/dir0002", 203},
		{"/dir0001/dir0002/.", 203},
		{"/dir0001/dir0002/..", 202},
		{"/dir0001/dir0002/file.0002.txt", 3},
		{"/dir0001/dir0002/file.0003.txt", 5},
		{"/dir0001/dir0002/file.0004.txt", 7},
		{"/dir0001/dir0002/file.0005.txt", 9},
		{"/dir0001/dir0002/file.0006.txt", 11},
		{"/dir0001/dir0002/file.0007.txt", 13},
		{"/dir0001/dir0002/file.0008.txt", 15},
		{"/dir0001/dir0002/file.0009.txt", 17},
		{"/dir0001/dir0002/file.0010.txt", 19},
		{"/dir0001/dir0002/file.0011.txt", 21},
		{"/dir0001/dir0002/file.0012.txt", 23},
		{"/dir0001/dir0002/file.0013.txt", 25},
		{"/dir0001/dir0002/file.0014.txt", 27},
		{"/dir0001/dir0002/file.0015.txt", 29},
		{"/dir0001/dir0002/file.0016.txt", 31},
		{"/dir0001/dir0002/file.0017.txt", 33},
		{"/dir0001/dir0002/file.0018.txt", 35},
		{"/dir0001/dir0002/file.0019.txt", 37},
		{"/dir0001/dir0002/file.0020.txt", 39},
		{"/dir0001/dir0002/file.0021.txt", 41},
		{"/dir0001/dir0002/file.0022.txt", 43},
		{"/dir0001/dir0002/file.0023.txt", 45},
		{"/dir0001/dir0002/file.0024.txt", 47},
		{"/dir0001/dir0002/file.0025.txt", 49},
		{"/dir0001/dir0002/file.0026.txt", 51},
		{"/dir0001/dir0002/file.0027.txt", 53},
		{"/dir0001/dir0002/file.0028.txt", 55},
		{"/dir0001/dir0002/file.0029.txt", 57},
		{"/dir0001/dir0002/file.0030.txt", 59},
		{"/dir0001/dir0002/file.0031.txt", 61},
		{"/dir0001/dir0002/file.0032.txt", 63},
		{"/dir0001/dir0002/file.0033.txt", 65},
		{"/dir0001/dir0002/file.0034.txt", 67},
		{"/dir0001/dir0002/file.0035.txt", 69},
		{"/dir0001/dir0002/file.0036.txt", 71},
		{"/dir0001/dir0002/file.0037.txt", 73},
		{"/dir0001/dir0002/file.0038.txt", 75},
		{"/dir0001/dir0002/file.0039.txt", 77},
		{"/dir0001/dir0002/file.0040.txt", 79},
		{"/dir0001/dir0002/file.0041.txt", 81},
		{"/dir0001/dir0002/file.0042.txt", 83},
		{"/dir0001/dir0002/file.0043.txt", 85},
		{"/dir0001/dir0002/file.0044.txt", 87},
		{"/dir0001/dir0002/file.0045.txt", 89},
		{"/dir0001/dir0002/file.0046.txt", 91},
		{"/dir0001/dir0002/file.0047.txt", 93},
		{"/dir0001/dir0002/file.0048.txt", 95},
		{"/dir0001/dir0002/file.0049.txt", 97},
		{"/dir0001/dir0002/file.0050.txt", 99},
		{"/dir0001/dir0002/file.0051.txt", 101},
		{"/dir0001/dir0002/file.0052.txt", 103},
		{"/dir0001/dir0002/file.0053.txt", 105},
		{"/dir0001/dir0002/file.0054.txt", 107},
		{"/dir0001/dir0002/file.0055.txt", 109},
		{"/dir0001/dir0002/file.0056.txt", 111},
		{"/dir0001/dir0002/file.0057.txt", 113},
		{"/dir0001/dir0002/file.0058.txt", 115},
		{"/dir0001/dir0002/file.0059.txt", 117},
		{"/dir0001/dir0002/file.0060.txt", 119},
		{"/dir0001/dir0002/file.0061.txt", 121},
		{"/dir0001/dir0002/file.0062.txt", 123},
		{"/dir0001/dir0002/file.0063.txt", 125},
		{"/dir0001/dir0002/file.0064.txt", 127},
		{"/dir0001/dir0002/file.0065.txt", 129},
		{"/dir0001/dir0002/file.0066.txt", 131},
		{"/dir0001/dir0002/file.0067.txt", 133},
		{"/dir0001/dir0002/file.0068.txt", 135},
		{"/dir0001/dir0002/file.0069.txt", 137},
		{"/dir0001/dir0002/file.0070.txt", 139},
		{"/dir0001/dir0002/file.0071.txt", 141},
		{"/dir0001/dir0002/file.0072.txt", 143},
		{"/dir0001/dir0002/file.0073.txt", 145},
		{"/dir0001/dir0002/file.0074.txt", 147},
		{"/dir0001/dir0002/file.0075.txt", 149},
		{"/dir0001/dir0002/file.0076.txt", 151},
		{"/dir0001/dir0002/file.0077.txt", 153},
		{"/dir0001/dir0002/file.0078.txt", 155},
		{"/dir0001/dir0002/file.0079.txt", 157},
		{"/dir0001/dir0002/file.0080.txt", 159},
		{"/dir0001/dir0002/file.0081.txt", 161},
		{"/dir0001/dir0002/file.0082.txt", 163},
		{"/dir0001/dir0002/file.0083.txt", 165},
		{"/dir0001/dir0002/file.0084.txt", 167},
		{"/dir0001/dir0002/file.0085.txt", 169},
		{"/dir0001/dir0002/file.0086.txt", 171},
		{"/dir0001/dir0002/file.0087.txt", 173},
		{"/dir0001/dir0002/file.0088.txt", 175},
		{"/dir0001/dir0002/file.0089.txt", 177},
		{"/dir0001/dir0002/file.0090.txt", 179},
		{"/dir0001/dir0002/file.0091.txt", 181},
		{"/dir0001/dir0002/file.0092.txt", 183},
		{"/dir0001/dir0002/file.0093.txt", 185},
		{"/dir0001/dir0002/file.0094.txt", 187},
		{"/dir0001/dir0002/file.0095.txt", 189},
		{"/dir0001/dir0002/file.0096.txt", 191},
		{"/dir0001/dir0002/file.0097.txt", 193},
		{"/dir0001/dir0002/file.0098.txt", 195},
		{"/dir0001/dir0002/file.0099.txt", 197},
		{"/dir0001/dir0002/file.0100.txt", 199},
		{"/dir0001/dir0002/file.0101.txt", 201},
	}

	i := 0
	for test := range testoutput {
		if test.Path != tests[i].Path {
			t.Fatalf("expected different path %q != %q", test.Path, tests[i].Path)
		}
		if test.Node.Ino != tests[i].Ino {
			t.Fatalf("expected different Inode Number (Ino) %d != %d",
				test.Node.Ino, tests[i].Ino)
		}
		i++
	}
}
