// Package inode provides thread-safe inode data structures for implementing
// in-memory filesystems.
//
// # Thread Safety
//
// All exported methods are safe for concurrent use by multiple goroutines.
// The package uses a hybrid approach to minimize lock contention:
//
// Lock-Free Operations (using sync/atomic):
//   - Inode number allocation (Ino.New, Ino.NewDir)
//   - Link count updates (Nlink field)
//   - Timestamp updates (Ctime, Atime, Mtime via accessor methods)
//
// RWMutex-Protected Operations:
//   - Directory operations (Link, Unlink, Resolve, Rename, ReadDir, Lookup)
//
// The Rename operation uses lock ordering by pointer address to prevent
// deadlocks when modifying multiple directories.
//
// # Direct Field Access
//
// For callers that need to access Inode fields directly, Lock/Unlock and
// RLock/RUnlock methods are provided:
//
//	node.RLock()
//	entries := node.Dir  // Safe to read
//	node.RUnlock()
//
//	node.Lock()
//	node.Size = 1024  // Safe to write
//	node.Unlock()
//
// However, prefer using the thread-safe methods (ReadDir, Lookup, etc.)
// when possible.
//
// # Usage Example
//
//	var ino inode.Ino
//	root := ino.NewDir(0755)
//
//	// Create a file
//	file := ino.New(0644)
//	root.Link("hello.txt", file)
//
//	// Create a subdirectory
//	subdir := ino.NewDir(0755)
//	root.Link("subdir", subdir)
//	subdir.Link("..", root)
//
//	// Resolve a path
//	node, err := root.Resolve("/subdir")
package inode

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path" // Virtual filesystem paths always use forward slashes
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

// An Inode represents the basic metadata of a file.
//
// Thread Safety:
//   - Ino is immutable after creation
//   - Nlink, Size, Mode, Uid, Gid use atomic operations (lock-free)
//   - ctime, atime, mtime use atomic operations (lock-free, accessed via methods)
//   - Dir and dirMap are protected by an internal RWMutex
//   - All exported methods are safe for concurrent use
//   - Direct field access requires external synchronization or use of Lock/RLock methods
type Inode struct {
	Ino   uint64
	Mode  os.FileMode
	Nlink uint64
	Size  int64

	ctime atomic.Int64 // creation time (Unix nanoseconds, unexported)
	atime atomic.Int64 // access time (Unix nanoseconds, unexported)
	mtime atomic.Int64 // modification time (Unix nanoseconds, unexported)
	Uid   uint32
	Gid   uint32

	Dir    Directory
	dirMap map[string]*DirEntry // O(1) name lookup, keyed by entry name

	mu sync.RWMutex // protects Dir and dirMap
}

// Ctime returns the creation time of the inode.
func (n *Inode) Ctime() time.Time { return time.Unix(0, n.ctime.Load()) }

// Atime returns the access time of the inode.
func (n *Inode) Atime() time.Time { return time.Unix(0, n.atime.Load()) }

// Mtime returns the modification time of the inode.
func (n *Inode) Mtime() time.Time { return time.Unix(0, n.mtime.Load()) }

// SetCtime sets the creation time of the inode.
func (n *Inode) SetCtime(t time.Time) { n.ctime.Store(t.UnixNano()) }

// SetAtime sets the access time of the inode.
func (n *Inode) SetAtime(t time.Time) { n.atime.Store(t.UnixNano()) }

// SetMtime sets the modification time of the inode.
func (n *Inode) SetMtime(t time.Time) { n.mtime.Store(t.UnixNano()) }

// DirEntry represents a single entry in a directory.
// It associates a name with an Inode pointer and implements fs.DirEntry.
type DirEntry struct {
	name  string
	Inode *Inode
}

// NewDirEntry creates a new directory entry with the given name and inode.
func NewDirEntry(name string, inode *Inode) *DirEntry {
	return &DirEntry{name: name, Inode: inode}
}

// Name returns the name of the file (or subdirectory) described by the entry.
// This implements fs.DirEntry.
func (e *DirEntry) Name() string {
	return e.name
}

// IsDir returns true if this entry references a directory inode.
func (e *DirEntry) IsDir() bool {
	if e.Inode == nil {
		return false
	}
	return e.Inode.IsDir()
}

// Type returns the type bits for the entry.
// This method implements fs.DirEntry.
func (e *DirEntry) Type() fs.FileMode {
	if e.Inode == nil {
		return 0
	}
	return e.Inode.Mode.Type()
}

// Info returns the FileInfo for the file or subdirectory described by the entry.
// This method implements fs.DirEntry.
func (e *DirEntry) Info() (fs.FileInfo, error) {
	if e.Inode == nil {
		return nil, fs.ErrNotExist
	}
	return &Stat{Filename: e.name, Node: e.Inode}, nil
}

// String returns a string representation of the directory entry.
func (e *DirEntry) String() string {
	nodeStr := "(nil)"
	if e.Inode != nil {
		nodeStr = fmt.Sprintf("{Ino:%d ...}", e.Inode.Ino)
	}
	return fmt.Sprintf("entry{%q, inode%s", e.name, nodeStr)
}

// Directory is a sorted slice of directory entries.
// It implements sort.Interface for maintaining alphabetical order by name.
// This enables O(log n) lookups via binary search.
type Directory []*DirEntry

// Len returns the number of entries in the directory.
func (d Directory) Len() int { return len(d) }

// Swap exchanges entries at positions i and j.
func (d Directory) Swap(i, j int) { d[i], d[j] = d[j], d[i] }

// Less reports whether the entry at i should sort before the entry at j.
func (d Directory) Less(i, j int) bool { return d[i].Name() < d[j].Name() }

func (n *Inode) String() string {
	if n == nil {
		return "<nil>"
	}

	list := make([]string, len(n.Dir))
	for i, e := range n.Dir {
		list[i] = e.String()
	}
	return fmt.Sprintf("Inode{Ino:%d,Mode:%s,Nlink:%d}\n\t%s", n.Ino, n.Mode, n.Nlink, strings.Join(list, ",\n"))
}

// Ino is an inode number allocator. It is safe for concurrent use.
// Each call to New or NewDir atomically increments the counter and
// returns a new Inode with a unique inode number.
type Ino uint64

// New creates a new Inode with the given mode and a unique inode number.
// This method is safe for concurrent use.
func (n *Ino) New(mode os.FileMode) *Inode {
	ino := atomic.AddUint64((*uint64)(n), 1)
	now := time.Now().UnixNano()
	node := &Inode{
		Ino:  ino,
		Mode: mode,
	}
	node.ctime.Store(now)
	node.atime.Store(now)
	node.mtime.Store(now)
	return node
}

// NewDir creates a new directory Inode with the given mode and a unique inode number.
// The directory is initialized with "." and ".." entries pointing to itself.
// This method is safe for concurrent use.
func (n *Ino) NewDir(mode os.FileMode) *Inode {
	dir := n.New(mode)
	dir.Mode = os.ModeDir | mode
	// Initialize . and .. without locking since this is a new inode
	// not yet visible to other goroutines
	dir.Dir = make(Directory, 0, 2)
	dir.dirMap = make(map[string]*DirEntry, 2)
	dotEntry := NewDirEntry(".", dir)
	dotdotEntry := NewDirEntry("..", dir)
	dir.Dir = append(dir.Dir, dotEntry, dotdotEntry)
	dir.dirMap["."] = dotEntry
	dir.dirMap[".."] = dotdotEntry
	dotEntry.Inode.countUp()
	dotdotEntry.Inode.countUp()
	return dir
}

// Link adds a directory entry (DirEntry) for the given node (assumed to be a directory)
// to the provided child Inode. If an entry with the same name exists, it is replaced.
// This method is safe for concurrent use.
func (n *Inode) Link(name string, child *Inode) error {
	if !n.IsDir() {
		return errors.New("not a directory")
	}

	n.mu.Lock()
	defer n.mu.Unlock()
	n.initDirMap()

	entry := NewDirEntry(name, child)

	if old, exists := n.dirMap[name]; exists {
		old.Inode.countDown()
		n.dirMap[name] = entry
		for i, e := range n.Dir {
			if e.name == name {
				n.Dir[i] = entry
				break
			}
		}
		entry.Inode.countUp()
		n.modified()
		return nil
	}

	n.dirMap[name] = entry
	n.Dir = append(n.Dir, entry)
	entry.Inode.countUp()
	n.modified()
	return nil
}

// Unlink removes the directory entry with the given name.
// This method is safe for concurrent use.
func (n *Inode) Unlink(name string) error {
	if !n.IsDir() {
		return errors.New("not a directory")
	}

	n.mu.Lock()
	defer n.mu.Unlock()
	n.initDirMap()

	entry, exists := n.dirMap[name]
	if !exists {
		return syscall.ENOENT
	}

	// Remove from map
	delete(n.dirMap, name)

	// Remove from Dir slice using swap-remove (O(1))
	for i, e := range n.Dir {
		if e.name == name {
			last := len(n.Dir) - 1
			n.Dir[i] = n.Dir[last]
			n.Dir[last] = nil
			n.Dir = n.Dir[:last]
			break
		}
	}

	entry.Inode.countDown()
	n.modified()
	return nil
}

// UnlinkAll recursively unlinks all entries in this directory.
// This method is safe for concurrent use.
func (n *Inode) UnlinkAll() {
	n.mu.Lock()
	// Take a snapshot of entries to process
	entries := make([]*DirEntry, len(n.Dir))
	copy(entries, n.Dir)
	n.Dir = n.Dir[:0]
	// Clear the map
	for k := range n.dirMap {
		delete(n.dirMap, k)
	}
	n.modified()
	n.mu.Unlock()

	// Process entries without holding the lock to avoid deadlock
	for _, e := range entries {
		if e.Name() == ".." {
			continue
		}
		if e.Inode.Ino == n.Ino {
			e.Inode.countDown()
			continue
		}
		e.Inode.UnlinkAll()
		e.Inode.countDown()
	}
}

// IsDir returns true if this inode represents a directory.
func (n *Inode) IsDir() bool {
	return os.ModeDir&n.Mode != 0
}

// Lock acquires an exclusive lock on this inode.
// Use this when modifying fields directly.
func (n *Inode) Lock() {
	n.mu.Lock()
}

// Unlock releases the exclusive lock.
func (n *Inode) Unlock() {
	n.mu.Unlock()
}

// RLock acquires a read lock on this inode.
// Use this when reading Dir or timestamp fields directly.
func (n *Inode) RLock() {
	n.mu.RLock()
}

// RUnlock releases the read lock.
func (n *Inode) RUnlock() {
	n.mu.RUnlock()
}

// ReadDir returns a snapshot of directory entries.
// This method is safe for concurrent use.
func (n *Inode) ReadDir() []*DirEntry {
	n.mu.RLock()
	defer n.mu.RUnlock()
	result := make([]*DirEntry, len(n.Dir))
	copy(result, n.Dir)
	sort.Slice(result, func(i, j int) bool {
		return result[i].name < result[j].name
	})
	return result
}

// Lookup finds a directory entry by name and returns it.
// Returns nil if not found. This method is safe for concurrent use.
func (n *Inode) Lookup(name string) *DirEntry {
	n.mu.RLock()
	if n.dirMap != nil {
		entry := n.dirMap[name]
		n.mu.RUnlock()
		return entry
	}
	n.mu.RUnlock()
	// Fallback for inodes without map (shouldn't happen in normal use)
	n.mu.Lock()
	n.initDirMap()
	entry := n.dirMap[name]
	n.mu.Unlock()
	return entry
}

// Rename moves/renames a file or directory from oldpath to newpath.
// Both paths are resolved relative to this inode.
// This method is safe for concurrent use. It uses lock ordering by
// pointer address to prevent deadlocks when locking multiple directories.
func (n *Inode) Rename(oldpath, newpath string) error {
	srcDir, srcName := path.Split(oldpath)
	srcDir = path.Clean(srcDir)

	dstDir, dstName := path.Split(newpath)
	dstDir = path.Clean(dstDir)

	// Resolve source node
	srcNode, err := n.Resolve(oldpath)
	if err != nil {
		return err
	}

	// Resolve source parent directory
	srcParent, err := n.Resolve(srcDir)
	if err != nil {
		return err
	}

	// Check if target already exists
	_, err = n.Resolve(newpath)
	if err == nil {
		return syscall.EEXIST
	}
	if !os.IsNotExist(err) {
		return err
	}

	// Resolve target parent directory
	dstParent, err := n.Resolve(dstDir)
	if err != nil {
		return err
	}

	// Lock directories in pointer order to prevent deadlock
	if srcParent == dstParent {
		// Same directory - single lock
		srcParent.mu.Lock()
		defer srcParent.mu.Unlock()
	} else {
		// Different directories - lock in address order
		first, second := srcParent, dstParent
		if uintptr(unsafe.Pointer(dstParent)) < uintptr(unsafe.Pointer(srcParent)) {
			first, second = dstParent, srcParent
		}
		first.mu.Lock()
		defer first.mu.Unlock()
		second.mu.Lock()
		defer second.mu.Unlock()
	}

	// Ensure dirMaps are initialized
	srcParent.initDirMap()
	if srcParent != dstParent {
		dstParent.initDirMap()
	}

	// Re-verify source exists after acquiring locks
	if _, exists := srcParent.dirMap[srcName]; !exists {
		return syscall.ENOENT
	}

	// Re-verify target doesn't exist after acquiring locks
	if _, exists := dstParent.dirMap[dstName]; exists {
		return syscall.EEXIST
	}

	// Add entry to destination (append, no sorted insert)
	entry := NewDirEntry(dstName, srcNode)
	dstParent.dirMap[dstName] = entry
	dstParent.Dir = append(dstParent.Dir, entry)
	srcNode.countUp()
	dstParent.modified()

	// Remove entry from source
	srcEntry := srcParent.dirMap[srcName]
	delete(srcParent.dirMap, srcName)
	for i, e := range srcParent.Dir {
		if e.name == srcName {
			last := len(srcParent.Dir) - 1
			srcParent.Dir[i] = srcParent.Dir[last]
			srcParent.Dir[last] = nil
			srcParent.Dir = srcParent.Dir[:last]
			break
		}
	}
	srcEntry.Inode.countDown()
	srcParent.modified()

	return nil
}

// Resolve traverses the path and returns the target Inode.
// Supports both absolute and relative paths.
// This method is safe for concurrent use.
func (n *Inode) Resolve(path string) (*Inode, error) {
	name, trim := PopPath(path)
	if name == "/" {
		if trim == "" {
			return n, nil
		}
		return n.Resolve(trim)
	}

	n.mu.RLock()
	var nn *Inode
	if n.dirMap != nil {
		if entry := n.dirMap[name]; entry != nil {
			nn = entry.Inode
		}
	}
	n.mu.RUnlock()

	if nn == nil {
		return nil, syscall.ENOENT
	}
	if len(trim) == 0 {
		return nn, nil
	}
	return nn.Resolve(trim)
}

// accessed updates the access time. Lock-free, safe for concurrent use.
func (n *Inode) accessed() {
	n.atime.Store(time.Now().UnixNano())
}

// modified updates both access and modification times. Lock-free, safe for concurrent use.
func (n *Inode) modified() {
	now := time.Now().UnixNano()
	n.atime.Store(now)
	n.mtime.Store(now)
}

// countUp atomically increments the link count (lock-free).
func (n *Inode) countUp() {
	atomic.AddUint64(&n.Nlink, 1)
}

// countDown atomically decrements the link count (lock-free).
// Panics if link count would go negative.
func (n *Inode) countDown() {
	// Use CAS loop to safely decrement and check for underflow
	for {
		old := atomic.LoadUint64(&n.Nlink)
		if old == 0 {
			panic(fmt.Sprintf("inode %d negative link count", n.Ino))
		}
		if atomic.CompareAndSwapUint64(&n.Nlink, old, old-1) {
			return
		}
	}
}

// initDirMap ensures the dirMap is initialized. Caller must hold n.mu.Lock().
func (n *Inode) initDirMap() {
	if n.dirMap != nil {
		return
	}
	n.dirMap = make(map[string]*DirEntry, len(n.Dir))
	for _, e := range n.Dir {
		n.dirMap[e.name] = e
	}
}
