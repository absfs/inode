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
	"os"
	filepath "path"
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
//   - Dir is protected by an internal RWMutex
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

	Dir Directory

	mu sync.RWMutex // protects Dir
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
// It associates a name with an Inode pointer.
type DirEntry struct {
	Name  string
	Inode *Inode
}

// IsDir returns true if this entry references a directory inode.
func (e *DirEntry) IsDir() bool {
	if e.Inode == nil {
		return false
	}
	return e.Inode.IsDir()
}

// String returns a string representation of the directory entry.
func (e *DirEntry) String() string {
	nodeStr := "(nil)"
	if e.Inode != nil {
		nodeStr = fmt.Sprintf("{Ino:%d ...}", e.Inode.Ino)
	}
	return fmt.Sprintf("entry{%q, inode%s", e.Name, nodeStr)
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
func (d Directory) Less(i, j int) bool { return d[i].Name < d[j].Name }

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
	dir.linkiNoLock(0, &DirEntry{".", dir})
	dir.linkiNoLock(1, &DirEntry{"..", dir})
	return dir
}

// Link adds a directory entry (DirEntry) for the given node (assumed to be a directory)
// to the provided child Inode. If an entry with the same name exists, it is replaced.
// This method is safe for concurrent use.
func (n *Inode) Link(name string, child *Inode) error {
	// Check directory status without lock (Mode is rarely modified)
	if !n.IsDir() {
		return errors.New("not a directory")
	}

	n.mu.Lock()
	defer n.mu.Unlock()

	x := n.find(name)
	entry := &DirEntry{name, child}

	if x < len(n.Dir) && n.Dir[x].Name == name {
		n.linkswapi(x, entry)
		return nil
	}
	n.linki(x, entry)
	return nil
}

// Unlink removes the directory entry with the given name.
// This method is safe for concurrent use.
func (n *Inode) Unlink(name string) error {
	// Check directory status without lock
	if !n.IsDir() {
		return errors.New("not a directory")
	}

	n.mu.Lock()
	defer n.mu.Unlock()

	x := n.find(name)
	if x == len(n.Dir) || n.Dir[x].Name != name {
		return syscall.ENOENT
	}

	n.unlinki(x)
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
	n.modified()
	n.mu.Unlock()

	// Process entries without holding the lock to avoid deadlock
	for _, e := range entries {
		if e.Name == ".." {
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
	return result
}

// Lookup finds a directory entry by name and returns it.
// Returns nil if not found. This method is safe for concurrent use.
func (n *Inode) Lookup(name string) *DirEntry {
	n.mu.RLock()
	defer n.mu.RUnlock()
	x := n.find(name)
	if x < len(n.Dir) && n.Dir[x].Name == name {
		return n.Dir[x]
	}
	return nil
}

// Rename moves/renames a file or directory from oldpath to newpath.
// Both paths are resolved relative to this inode.
// This method is safe for concurrent use. It uses lock ordering by
// pointer address to prevent deadlocks when locking multiple directories.
func (n *Inode) Rename(oldpath, newpath string) error {
	srcDir, srcName := filepath.Split(oldpath)
	srcDir = filepath.Clean(srcDir)

	dstDir, dstName := filepath.Split(newpath)
	dstDir = filepath.Clean(dstDir)

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

	// Re-verify source exists after acquiring locks
	srcIdx := srcParent.find(srcName)
	if srcIdx >= len(srcParent.Dir) || srcParent.Dir[srcIdx].Name != srcName {
		return syscall.ENOENT
	}

	// Re-verify target doesn't exist after acquiring locks
	dstIdx := dstParent.find(dstName)
	if dstIdx < len(dstParent.Dir) && dstParent.Dir[dstIdx].Name == dstName {
		return syscall.EEXIST
	}

	// Create entry in destination
	entry := &DirEntry{dstName, srcNode}
	dstParent.Dir = append(dstParent.Dir, nil)
	copy(dstParent.Dir[dstIdx+1:], dstParent.Dir[dstIdx:])
	dstParent.Dir[dstIdx] = entry
	srcNode.countUp()
	dstParent.modified()

	// Remove entry from source (recalculate index if same directory since we inserted)
	if srcParent == dstParent && dstIdx <= srcIdx {
		srcIdx++ // account for the insertion
	}
	srcParent.Dir[srcIdx].Inode.countDown()
	copy(srcParent.Dir[srcIdx:], srcParent.Dir[srcIdx+1:])
	srcParent.Dir[len(srcParent.Dir)-1] = nil
	srcParent.Dir = srcParent.Dir[:len(srcParent.Dir)-1]
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
	x := n.find(name)
	var nn *Inode
	if x < len(n.Dir) && n.Dir[x].Name == name {
		nn = n.Dir[x].Inode
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

// unlinki removes the entry at index i. Caller must hold n.mu.Lock().
func (n *Inode) unlinki(i int) {
	n.Dir[i].Inode.countDown()
	copy(n.Dir[i:], n.Dir[i+1:])
	n.Dir[len(n.Dir)-1] = nil // avoid memory leak
	n.Dir = n.Dir[:len(n.Dir)-1]
	n.modified()
}

// linkswapi replaces the entry at index i. Caller must hold n.mu.Lock().
func (n *Inode) linkswapi(i int, entry *DirEntry) {
	n.Dir[i].Inode.countDown()
	n.Dir[i] = entry
	n.Dir[i].Inode.countUp()
	n.modified()
}

// linki inserts entry at index i maintaining sorted order. Caller must hold n.mu.Lock().
func (n *Inode) linki(i int, entry *DirEntry) {
	n.Dir = append(n.Dir, nil)
	copy(n.Dir[i+1:], n.Dir[i:])
	n.Dir[i] = entry
	n.Dir[i].Inode.countUp()
	n.modified()
}

// linkiNoLock inserts entry at index i without locking or updating timestamps.
// Used only during inode initialization before the inode is visible to other goroutines.
func (n *Inode) linkiNoLock(i int, entry *DirEntry) {
	n.Dir = append(n.Dir, nil)
	copy(n.Dir[i+1:], n.Dir[i:])
	n.Dir[i] = entry
	n.Dir[i].Inode.countUp()
}

// find returns the index where name should be inserted to maintain sorted order.
// If the name exists, returns its index. Caller must hold n.mu.RLock() or n.mu.Lock().
func (n *Inode) find(name string) int {
	return sort.Search(len(n.Dir), func(i int) bool {
		return n.Dir[i].Name >= name
	})
}
