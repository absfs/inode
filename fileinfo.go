package inode

import (
	"os"
	"time"
)

type info struct {
	name string
	node *Inode
}

// base name of the file
func (i *info) Name() string {
	return i.name
}

// length in bytes for regular files; system-dependent for others
func (i *info) Size() int64 {
	return i.node.Size
}

// file mode bits
func (i *info) Mode() os.FileMode {
	return i.node.Mode
}

// modification time
func (i *info) ModTime() time.Time {
	return i.node.Mtime
}

// abbreviation for Mode().IsDir()
func (i *info) IsDir() bool {
	return i.Mode()&os.ModeDir != 0
}

// underlying data source (can return nil)
func (i *info) Sys() interface{} {
	return i.node
}
