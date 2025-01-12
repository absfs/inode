package inode

import (
	"errors"
	"strings"
)

// Walk traverses a directory tree and calls fn for each file/directory encountered
func Walk(node *Inode, path string, fn func(path string, n *Inode) error) error {
	err := fn(path, node)
	if err != nil {
		return err
	}

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
		err := Walk(entry.Inode, path+"/"+entry.Name, fn)
		if err != nil {
			return err
		}
	}
	return nil
}
