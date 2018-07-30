# inode - filesystem primitives for go
The `inode` package provides `Inode`, `Directory`, and `DirEntry` primitives
for inode style filesystems. This package does not implement any actual data
storage features, only the data structures and logic needed to implement a
storage system.

```go
import "github.com/absfs/inode"
```

## Example Usage
The basic idea is to use the `inode` package to handle file path resolution,
and directory entries. The use the inode number `Ino` to point to custom
data structures in the client side package.  Here's a simplified example using
a slice of `[]byte` for file data storage.

See "github.com/absfs/memfs" for a more complete example of usage in the
creation of an in memory filesystem.


```go

type SimpleFS struct {
  Ino  *inode.Ino
  Root *inode.Inode
  Data map[uint64][]byte
}

type SimpleFile struct {
  fs       *SimpleFS
  readonly bool
  node     *inode.Inode
  name     string
  data     []byte
}

func (fs *SimpleFS) Mkdir(name string, mode os.FileMode) error {
  child, err := fs.Root.Resolve(name)
  if err == nil {
    return os.ErrExist
  }

  dir, filename := filepath.Split(name)
  dir = filepath.Clean(dir)

  parent, err := fs.Root.Resolve(dir)
  if err != nil {
    return err
  }

  child = fs.Ino.NewDir(mode)
  parent.Link(filename, child)
  child.Link("..", parent)
  return nil
}

func (fs *SimpleFS) Create(name string) (f *SimpleFile, err error) {
  node, err := fs.Root.Resolve(name)
  if node != nil && err == nil {

    return &SimpleFile{
      fs:   fs,
      name: name,
      node: node,
      data: fs.Data[node.Ino],
    }, nil
  }

  dir, filename := filepath.Split(name)
  dir = filepath.Clean(dir)
  parent, err := fs.Root.Resolve(dir)
  if err != nil {
    return nil, err
  }

  node = fs.Ino.New(0644)
  fs.Data[node.Ino] = []byte{}
  parent.Link(filename, node)
  node.Link("..", parent)
  return &SimpleFile{
    fs:   fs,
    name: name,
    node: node,
    data: fs.Data[node.Ino],
  }, nil
}

func (fs *SimpleFS) Open(name string) (f *SimpleFile, err error) {
  node, err := fs.Root.Resolve(name)
  if err != nil {
    return nil, err
  }

  return &SimpleFile{
    fs:       fs,
    readonly: true,
    name:     name,
    node:     node,
    data:     fs.Data[node.Ino],
  }, nil
}

func (f *SimpleFile) Write(p []byte) (n int, err error) {
  if f.readonly {
    return 0, os.ErrPermission
  }

  f.data = append(f.data, p...)
  return len(p), nil
}

func (f *SimpleFile) Read(p []byte) (n int, err error) {
  n = copy(p, f.data)
  return n, nil
}

func (f *SimpleFile) Close() error {
  if !f.readonly {
    f.fs.Data[f.node.Ino] = f.data
  }
  return nil
}

func main() {

  ino := new(inode.Ino)
  fs := &SimpleFS{
    Ino:  ino,
    Root: ino.NewDir(0755),
    Data: make(map[uint64][]byte),
  }
  err := fs.Mkdir("/tmp", 0755)
  if err != nil {
    panic(err)
  }
  f, err := fs.Create("/tmp/testfile.txt")
  if err != nil {
    panic(err)
  }

  n, err := f.Write([]byte("Hello, World!\n"))
  if err != nil {
    panic(err)
  }
  f.Close()

  f, err = fs.Open("/tmp/testfile.txt")
  if err != nil {
    panic(err)
  }

  buffer := make([]byte, 512)
  n, err = f.Read(buffer)
  if err != nil {
    panic(err)
  }
  buffer = buffer[:n]
  if string(buffer) != "Hello, World!\n" {
    fmt.Fprintf(os.Stderr, "error buffer == %q\n", string(buffer))
  }
  fmt.Printf("%s", string(buffer))
}

```

## LICENSE

This project is governed by the MIT License. See [LICENSE](https://github.com/absfs/inode/blob/master/LICENSE)

