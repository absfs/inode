package inode

import (
	"os"
	"testing"
	"time"
)

func TestStat_Name(t *testing.T) {
	var ino Ino = 1
	node := ino.New(os.ModePerm)
	stat := &Stat{
		Filename: "testfile.txt",
		Node:     node,
	}

	if got := stat.Name(); got != "testfile.txt" {
		t.Errorf("Name() = %q, want %q", got, "testfile.txt")
	}
}

func TestStat_Size(t *testing.T) {
	var ino Ino = 1
	node := ino.New(os.ModePerm)
	node.Size = 1024

	stat := &Stat{
		Filename: "testfile.txt",
		Node:     node,
	}

	if got := stat.Size(); got != 1024 {
		t.Errorf("Size() = %d, want %d", got, 1024)
	}
}

func TestStat_Mode(t *testing.T) {
	tests := []struct {
		name string
		mode os.FileMode
	}{
		{"regular file", 0644},
		{"directory", os.ModeDir | 0755},
		{"executable", 0755},
		{"symlink", os.ModeSymlink | 0777},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ino Ino = 1
			node := ino.New(tt.mode)
			stat := &Stat{
				Filename: "test",
				Node:     node,
			}

			if got := stat.Mode(); got != tt.mode {
				t.Errorf("Mode() = %v, want %v", got, tt.mode)
			}
		})
	}
}

func TestStat_ModTime(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	var ino Ino = 1
	node := ino.New(os.ModePerm)
	node.SetMtime(now)

	stat := &Stat{
		Filename: "testfile.txt",
		Node:     node,
	}

	if got := stat.ModTime(); !got.Equal(now) {
		t.Errorf("ModTime() = %v, want %v", got, now)
	}
}

func TestStat_IsDir(t *testing.T) {
	tests := []struct {
		name   string
		mode   os.FileMode
		expect bool
	}{
		{"directory", os.ModeDir | 0755, true},
		{"regular file", 0644, false},
		{"executable file", 0755, false},
		{"symlink", os.ModeSymlink | 0777, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ino Ino = 1
			node := ino.New(tt.mode)
			stat := &Stat{
				Filename: "test",
				Node:     node,
			}

			if got := stat.IsDir(); got != tt.expect {
				t.Errorf("IsDir() = %v, want %v", got, tt.expect)
			}
		})
	}
}

func TestStat_Sys(t *testing.T) {
	var ino Ino = 1
	node := ino.New(os.ModePerm)
	stat := &Stat{
		Filename: "testfile.txt",
		Node:     node,
	}

	got := stat.Sys()
	if got != node {
		t.Errorf("Sys() = %v, want %v", got, node)
	}

	// Verify it returns the actual Inode pointer
	if gotNode, ok := got.(*Inode); !ok || gotNode != node {
		t.Errorf("Sys() did not return the expected *Inode")
	}
}

func TestStat_FileInfoInterface(t *testing.T) {
	// Verify that Stat implements os.FileInfo interface
	var _ os.FileInfo = &Stat{}

	// Create a Stat and use it as FileInfo
	var ino Ino = 123
	node := ino.New(os.ModeDir | 0755)
	node.Size = 4096
	node.SetMtime(time.Date(2025, 11, 23, 12, 0, 0, 0, time.UTC))

	stat := &Stat{
		Filename: "mydir",
		Node:     node,
	}

	var fi os.FileInfo = stat

	// Test all methods through the interface
	if fi.Name() != "mydir" {
		t.Errorf("FileInfo.Name() = %q, want %q", fi.Name(), "mydir")
	}

	if fi.Size() != 4096 {
		t.Errorf("FileInfo.Size() = %d, want %d", fi.Size(), 4096)
	}

	if fi.Mode() != (os.ModeDir | 0755) {
		t.Errorf("FileInfo.Mode() = %v, want %v", fi.Mode(), os.ModeDir|0755)
	}

	expectedTime := time.Date(2025, 11, 23, 12, 0, 0, 0, time.UTC)
	if !fi.ModTime().Equal(expectedTime) {
		t.Errorf("FileInfo.ModTime() = %v, want %v", fi.ModTime(), expectedTime)
	}

	if !fi.IsDir() {
		t.Errorf("FileInfo.IsDir() = false, want true")
	}

	if fi.Sys() != node {
		t.Errorf("FileInfo.Sys() = %v, want %v", fi.Sys(), node)
	}
}
