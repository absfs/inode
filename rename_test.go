package inode

import (
	"fmt"
	"strings"
	"syscall"
	"testing"
)

func TestRenameToExisting(t *testing.T) {
	ino := new(Ino)
	root := ino.NewDir(0777)

	// Create source file
	srcFile := ino.New(0666)
	err := root.Link("source.txt", srcFile)
	if err != nil {
		t.Fatal(err)
	}

	// Create target file
	targetFile := ino.New(0666)
	err = root.Link("target.txt", targetFile)
	if err != nil {
		t.Fatal(err)
	}

	// Attempt to rename source to existing target
	err = root.Rename("/source.txt", "/target.txt")
	if err != syscall.EEXIST {
		t.Errorf("Expected EEXIST error when renaming to existing target, got %v", err)
	}

	// Verify source and target files still exist
	_, err = root.Resolve("/source.txt")
	if err != nil {
		t.Error("Source file should still exist")
	}

	_, err = root.Resolve("/target.txt")
	if err != nil {
		t.Error("Target file should still exist")
	}
}

func TestLinkUnlinkMove(t *testing.T) {
	ino := new(Ino)
	root := ino.NewDir(0777)
	dirs := make([]*Inode, 2)
	var err error

	for i := range dirs {
		dirs[i] = ino.NewDir(0777)
		err = root.Link(fmt.Sprintf("dir%02d", i), dirs[i])
		if err != nil {
			t.Fatal(err)
		}
	}

	files := make([]*Inode, 2)
	for i := range files {
		files[i] = ino.New(0666)
		err = root.Link(fmt.Sprintf("file_%04d.txt", i), files[i])
		if err != nil {
			t.Fatal(err)
		}
	}
	list := []string{
		"/",
		"/dir00/",
		"/dir01/",
		"/file_0000.txt",
		"/file_0001.txt",
	}
	i := 0
	err = Walk(root, "/", func(path string, n *Inode) error {
		if strings.HasSuffix(path, "/..") || strings.HasSuffix(path, "/.") {
			return nil
		}
		if n.IsDir() && path != "/" {
			path += "/"
		}
		if list[i] != path {
			t.Fatalf("expected file listing to match %s != %s", list[i], path)
		}
		i++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	err = root.Rename("/file_0001.txt", "/dir01/file_0001.txt")
	if err != nil {
		t.Fatal(err)
	}

	list = []string{
		"/",
		"/dir00/",
		"/dir01/",
		"/dir01/file_0001.txt",
		"/file_0000.txt",
	}
	i = 0
	err = Walk(root, "/", func(path string, n *Inode) error {
		if strings.HasSuffix(path, "/..") || strings.HasSuffix(path, "/.") {
			return nil
		}
		if n.IsDir() && path != "/" {
			path += "/"
		}
		if list[i] != path {
			t.Fatalf("expected file listing to match %s != %s", list[i], path)
		}
		i++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// move with simultaneous rename
	err = root.Rename("/file_0000.txt", "/dir01/file_0003.txt")
	if err != nil {
		t.Fatal(err)
	}

	// Create dir01 inside dir00
	dir01 := ino.NewDir(0777)
	err = dirs[0].Link("dir01", dir01)
	if err != nil {
		t.Fatal(err)
	}
	err = dir01.Link("..", dirs[0])
	if err != nil {
		t.Fatal(err)
	}

	// Move files into the new directory
	err = root.Rename("/dir01/file_0001.txt", "/dir00/dir01/file_0001.txt")
	if err != nil {
		t.Fatal(err)
	}

	err = root.Rename("/dir01/file_0003.txt", "/dir00/dir01/file_0003.txt")
	if err != nil {
		t.Fatal(err)
	}

	// Remove old dir01
	err = root.Unlink("dir01")
	if err != nil {
		t.Fatal(err)
	}

	list = []string{
		"/",
		"/dir00/",
		"/dir00/dir01/",
		"/dir00/dir01/file_0001.txt",
		"/dir00/dir01/file_0003.txt",
	}
	i = 0
	err = Walk(root, "/", func(path string, n *Inode) error {
		if strings.HasSuffix(path, "/..") || strings.HasSuffix(path, "/.") {
			return nil
		}
		if n.IsDir() && path != "/" {
			path += "/"
		}
		if list[i] != path {
			t.Fatalf("expected file listing to match %s != %s", list[i], path)
		}
		i++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
