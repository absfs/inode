# BlockStore Interface Design - Implementation Summary

## What Was Delivered

This design work provides a comprehensive BlockStore interface architecture for the absfs/inode package, researched from real filesystem implementations and designed for Go.

### Files Created

1. **BLOCKSTORE_DESIGN.md** (Comprehensive Design Document)
   - Research on real filesystem patterns (ext4, ZFS, APFS, NTFS, FAT32)
   - Analysis of current memfs and boltfs implementations
   - Three interface design options with trade-offs
   - Lock-free design patterns using Go's sync/atomic
   - Integration recommendations with the inode package
   - Migration path and next steps

2. **blockstore.go** (Interface Definitions)
   - `ByteStore` interface - Simple byte-oriented storage (Option A)
   - `BlockChainStore` interface - Fixed-size block chains (Option B)
   - `ExtentStore` interface - Extent-based allocation (Option C)
   - `COWStore` interface - Copy-on-write extension
   - Helper types and utility functions
   - Comprehensive documentation with examples

3. **blockstore_examples.go** (Reference Implementations)
   - `MemByteStore` - In-memory ByteStore implementation
   - `MemBlockChainStore` - In-memory BlockChainStore implementation
   - `MemExtentStore` - In-memory ExtentStore implementation
   - `COWMemExtentStore` - Copy-on-write ExtentStore implementation
   - Demonstrates patterns for each interface

4. **blockstore_test.go** (Comprehensive Test Suite)
   - Tests for all three store types
   - Tests for sparse files (holes)
   - Tests for extent list operations
   - Tests for copy-on-write functionality
   - Tests for utility functions
   - Benchmarks for performance measurement

## Key Design Decisions

### 1. Three-Tier Approach

**ByteStore (Simple)**
- Matches current memfs/boltfs architecture
- Each file is a single variable-sized byte array
- Best for small files and in-memory systems
- Easy to implement and understand

**BlockChainStore (Intermediate)**
- Fixed-size blocks in a chain
- Supports sparse files (block ID 0 = hole)
- Enables incremental updates
- Classic Unix-style approach

**ExtentStore (Advanced)**
- Contiguous block ranges (extents)
- Most efficient for large sequential files
- Modern filesystem approach (ext4, XFS, APFS)
- Best scalability and performance

### 2. Lock-Free Design Patterns

Based on Go's `sync/atomic` research:
- Use `atomic.Pointer[T]` for extent list copy-on-write
- Use `atomic.Int64` for file size tracking
- Use `atomic.Uint64` for reference counts
- Enable lock-free reads with occasional atomic swaps
- Mutexes for complex structural modifications

### 3. Architecture Integration

**Recommended: FileSystem Owns BlockStore**
- Single block store instance per filesystem
- Passed to File handles for operations
- Enables centralized allocation
- Supports block-level deduplication

### 4. Migration Path

**Phase 1**: Implement ByteStore (formalize current pattern)
**Phase 2**: Add ExtentStore for advanced features
**Phase 3**: Enable COW for snapshots/clones
**Phase 4**: Disk-backed filesystem implementations

## Interface Highlights

### ByteStore - The Simple Path

```go
type ByteStore interface {
    ReadAt(ino uint64, p []byte, off int64) (n int, err error)
    WriteAt(ino uint64, p []byte, off int64) (n int, err error)
    Truncate(ino uint64, size int64) error
    Remove(ino uint64) error
    Stat(ino uint64) (size int64, err error)
}
```

**Pros**: Simple, matches current code, easy to implement
**Cons**: No sparse files, write amplification

### ExtentStore - The Modern Path

```go
type Extent struct {
    Logical  uint64  // Logical block in file
    Physical uint64  // Physical block ID
    Length   uint64  // Number of contiguous blocks
}

type ExtentStore interface {
    BlockSize() int
    AllocExtent(desiredLength uint64) (physical, length uint64, err error)
    FreeExtent(physical, length uint64) error
    ReadExtent(physical, length uint64) (data []byte, err error)
    WriteExtent(physical, length uint64, data []byte) error
    GetExtents(ino uint64) (ExtentList, error)
    SetExtents(ino uint64, extents ExtentList) error
}
```

**Pros**: Efficient, sparse files, scalable, COW-friendly
**Cons**: More complex, requires extent management

### COWStore - Advanced Features

```go
type COWStore interface {
    CloneExtents(srcIno, dstIno uint64) error
    BreakCOW(ino uint64, logicalBlock uint64) (physical uint64, err error)
    RefCount(physical uint64) uint64
}
```

Enables:
- Instant file clones (ZFS/APFS-style)
- Filesystem snapshots
- Space-efficient copies

## Test Results

All tests pass successfully:

```
PASS: TestMemByteStore
PASS: TestMemByteStoreSparseWrite
PASS: TestMemBlockChainStore
PASS: TestMemExtentStore
PASS: TestExtentListHelpers
PASS: TestBlockUtilities
PASS: TestCOWMemExtentStore
```

## Usage Example

```go
// Create a byte store
store := NewMemByteStore()

// Write data
data := []byte("hello world")
n, err := store.WriteAt(ino, data, 0)

// Read data
buf := make([]byte, 11)
n, err = store.ReadAt(ino, buf, 0)

// Sparse write (creates hole)
store.WriteAt(ino, []byte("test"), 1000)

// Truncate
store.Truncate(ino, 500)
```

## References

All research properly cited in the design document:
- [Ext4 Disk Layout](https://ext4.wiki.kernel.org/index.php/Ext4_Disk_Layout)
- [Extents in Ext4](https://blogs.oracle.com/linux/extents-and-extent-allocation-in-ext4)
- [ZFS Wikipedia](https://en.wikipedia.org/wiki/ZFS)
- [Understanding ZFS Block Sizes](https://ibug.io/blog/2023/10/zfs-block-size/)
- [Go sync/atomic package](https://pkg.go.dev/sync/atomic)
- [Atomic Operations in Go](https://medium.com/@hatronix/go-go-lockless-techniques-for-managing-state-and-synchronization-5398370c379b)
- [Lock-Free Data Structures in Go](https://dev.to/aaravjoshi/mastering-lock-free-data-structures-in-go-ring-buffers-queues-and-performance-optimization-2f2d)

## Next Steps

1. **Review**: Discuss design with team/stakeholders
2. **Implement**: Start with ByteStore in memfs
3. **Refactor**: Update memfs to use ByteStore interface
4. **Extend**: Add ExtentStore for boltfs improvements
5. **Advanced**: Implement COW for snapshots

## Benefits

1. **Clean Abstraction**: Separate concerns (metadata vs data storage)
2. **Flexibility**: Support multiple storage strategies
3. **Performance**: Lock-free reads, efficient large files
4. **Features**: Sparse files, snapshots, clones
5. **Future-Proof**: Foundation for disk-based filesystems

## Compatibility

- All interfaces are **new** - no breaking changes to existing code
- Current memfs/boltfs can continue working as-is
- Migration is opt-in and gradual
- Backward compatible with existing patterns

---

**Status**: ✅ Design Complete, Interfaces Defined, Tests Passing

**Ready For**: Review and implementation planning
