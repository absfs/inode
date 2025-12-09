# BlockStore Interface Design for absfs/inode

## Executive Summary

This document proposes a fundamental block storage abstraction for the absfs inode package. After researching modern filesystem designs (ext4, ZFS, APFS, NTFS) and analyzing the current memfs and boltfs implementations, we present three interface design options ranging from simple byte-oriented storage to sophisticated extent-based allocation.

## 1. Real Filesystem Block Storage Patterns

### 1.1 ext4 - Extent-Based Allocation

**Key Concepts:**
- **Extents**: A range of contiguous physical blocks mapped with a single structure
- A single extent can map up to 128 MiB (with 4 KiB blocks)
- Up to 4 extents stored directly in the inode
- **Extent Tree**: Interior nodes contain indices to child nodes; leaf nodes contain actual extents
- **Efficiency**: Mapping 1,000 contiguous blocks requires 1 extent vs 1,000 indirect block entries

**Traditional Indirect Blocks (ext2/ext3):**
- Direct blocks: First 12 blocks stored in inode
- Single indirect: Points to block containing 256 block addresses
- Double indirect: Points to block of single indirect blocks
- Triple indirect: Points to block of double indirect blocks
- Inefficient for large files due to massive mapping overhead

**Source:** [Ext4 Disk Layout](https://ext4.wiki.kernel.org/index.php/Ext4_Disk_Layout), [Extents in Ext4](https://blogs.oracle.com/linux/extents-and-extent-allocation-in-ext4)

### 1.2 ZFS - Variable Block Size with Copy-on-Write

**Key Concepts:**
- **Variable block sizes**: Up to 128 KiB default (max 1 MiB with large_blocks feature)
- **Copy-on-Write**: Modified blocks written to new location, old blocks freed
- **Smart block sizing**: Small files use single logical block rounded to 512-byte multiple
- **Compression-aware**: Compressed blocks use smaller physical allocation
- **Write amplification**: Modifying 8 KiB in 128 KiB block requires reading, modifying, and writing entire block

**COW Behavior:**
- Only modified blocks are copied
- Original blocks remain until new blocks are committed
- Enables atomic snapshots and rollback
- Block cloning for efficient file copies

**Source:** [ZFS Wikipedia](https://en.wikipedia.org/wiki/ZFS), [Understanding ZFS Block Sizes](https://ibug.io/blog/2023/10/zfs-block-size/)

### 1.3 APFS - Space Sharing and Snapshots

**Key Concepts:**
- Copy-on-write for metadata and data
- Space sharing between volumes in same container
- Clones are instantaneous (reference existing blocks)
- Extents for large files
- Sparse file support via extent gaps

### 1.4 NTFS - Cluster Runs and MFT

**Key Concepts:**
- **Cluster**: Smallest allocation unit (multiple sectors)
- **Runs**: Contiguous cluster sequences (similar to extents)
- **MFT (Master File Table)**: Each file has MFT record
- Small files stored entirely in MFT (resident data)
- Large files use runs of clusters (non-resident data)
- Sparse files: Unallocated runs marked as holes

### 1.5 FAT32 - Simple Cluster Chains

**Key Concepts:**
- Fixed cluster size (4 KiB to 32 KiB)
- File Allocation Table maps cluster to next cluster
- Simple linked list of clusters
- No extent concept - purely chain-based
- Fragmentation issues due to linked allocation

## 2. Current absfs Implementation Analysis

### 2.1 memfs - Simple Byte Array

**Current Implementation:**
```go
type FileSystem struct {
    data [][]byte  // fs.data[ino] = file contents
    // ...
}
```

**Characteristics:**
- Direct mapping: inode number → byte slice
- Entire file in single contiguous byte array
- No block concept - variable-size blobs
- Simple append/truncate operations
- Memory-efficient for small files, wasteful for sparse files
- No sharing between files (no COW/deduplication)

### 2.2 boltfs - Key-Value Storage

**Current Implementation:**
```go
// Metadata in "inodes" bucket
// Data in "data" bucket: key=ino, value=[]byte
// Optional external contentFS for data storage

func (fs *FileSystem) saveData(ino uint64, data []byte) error {
    if fs.contentFS != nil {
        // Store in external filesystem
        path := inoToPath(ino)  // /XX/XXXXXXXXXXXXXXXX
        // Write entire file
    } else {
        // Store in BoltDB data bucket
        b.data.Put(i2b(ino), data)
    }
}
```

**Characteristics:**
- Entire file stored as single value
- No block decomposition
- External storage option mirrors file-per-inode pattern
- No sparse file support
- No block-level deduplication
- No incremental updates (must write entire file)

### 2.3 inode Package - Directory-Only

**Current Implementation:**
- `Inode.Dir` contains directory entries
- No data storage mechanism
- Filesystems (memfs, boltfs) implement their own data storage
- Size tracked in `Inode.Size` field

## 3. Design Requirements

Based on the research and current implementation analysis:

### 3.1 Functional Requirements

1. **Support multiple filesystem patterns**: memfs (simple), boltfs (persistent), future disk-based implementations
2. **Sparse file support**: Efficiently handle files with holes
3. **Incremental updates**: Modify portions without rewriting entire file
4. **Variable file sizes**: From empty to multi-GB
5. **Copy-on-write option**: Enable snapshots/clones for advanced filesystems
6. **Atomic operations**: Leverage Go's sync/atomic where possible

### 3.2 Non-Functional Requirements

1. **Performance**: Minimal allocation overhead, cache-friendly access patterns
2. **Memory efficiency**: Don't waste memory on unused allocations
3. **Thread safety**: Safe for concurrent access (prefer atomics over locks)
4. **Simplicity**: Easy to implement for simple filesystems
5. **Extensibility**: Advanced features optional, not required

## 4. BlockStore Interface Options

### Option A: Simple Byte-Oriented Storage (Current Style)

**Philosophy:** Keep the current simple model, formalize it as an interface.

```go
package inode

import (
    "io"
    "sync/atomic"
)

// ByteStore provides simple byte-oriented file storage.
// Each inode has a single variable-sized byte array.
// This is the simplest model, best for in-memory filesystems.
type ByteStore interface {
    // ReadAt reads len(p) bytes from the file at offset off.
    // Returns number of bytes read and error (io.EOF at end).
    ReadAt(ino uint64, p []byte, off int64) (n int, err error)

    // WriteAt writes len(p) bytes to the file at offset off.
    // Extends file if necessary. Returns bytes written and error.
    WriteAt(ino uint64, p []byte, off int64) (n int, err error)

    // Truncate changes file size. Extends with zeros or truncates.
    Truncate(ino uint64, size int64) error

    // Remove deletes all data for the inode.
    Remove(ino uint64) error

    // Stat returns the current size of the file.
    Stat(ino uint64) (size int64, err error)
}

// memfs implementation
type memByteStore struct {
    mu   sync.RWMutex
    data map[uint64][]byte
}

func (s *memByteStore) ReadAt(ino uint64, p []byte, off int64) (int, error) {
    s.mu.RLock()
    defer s.mu.RUnlock()

    data := s.data[ino]
    if off >= int64(len(data)) {
        return 0, io.EOF
    }

    n := copy(p, data[off:])
    if n < len(p) {
        return n, io.EOF
    }
    return n, nil
}

func (s *memByteStore) WriteAt(ino uint64, p []byte, off int64) (int, error) {
    s.mu.Lock()
    defer s.mu.Unlock()

    data := s.data[ino]
    end := off + int64(len(p))

    if end > int64(len(data)) {
        newData := make([]byte, end)
        copy(newData, data)
        data = newData
        s.data[ino] = data
    }

    return copy(data[off:], p), nil
}

// boltfs implementation
type boltByteStore struct {
    db     *bolt.DB
    bucket string
}

func (s *boltByteStore) ReadAt(ino uint64, p []byte, off int64) (int, error) {
    var n int
    err := s.db.View(func(tx *bolt.Tx) error {
        data := tx.Bucket([]byte(s.bucket)).Get(i2b(ino))
        if off >= int64(len(data)) {
            return io.EOF
        }
        n = copy(p, data[off:])
        return nil
    })
    return n, err
}
```

**Pros:**
- Simplest to implement and understand
- Matches current memfs/boltfs architecture
- Low overhead for small files
- Easy to reason about concurrency

**Cons:**
- No sparse file support (wastes memory)
- Must read/write entire file for persistence
- No block-level deduplication
- Poor scalability for large files
- Write amplification on small changes

**Best For:**
- memfs (current implementation)
- Small to medium files
- Filesystems without persistence requirements

### Option B: Fixed-Size Block Chains (Classic Unix Style)

**Philosophy:** Break files into fixed-size blocks, chain them together. Enables incremental updates and sparse files.

```go
package inode

// BlockSize is the fixed block size (e.g., 4096 bytes)
const DefaultBlockSize = 4096

// Block represents a single fixed-size data block.
type Block struct {
    Data [DefaultBlockSize]byte
    // For linked-list implementation:
    // Next uint64  // block ID of next block (0 = end)
}

// BlockChainStore provides fixed-size block storage with chaining.
// Files are sequences of fixed-size blocks.
type BlockChainStore interface {
    // BlockSize returns the block size for this store.
    BlockSize() int

    // AllocBlock allocates a new block and returns its ID.
    AllocBlock() (blockID uint64, err error)

    // FreeBlock releases a block for reuse.
    FreeBlock(blockID uint64) error

    // ReadBlock reads an entire block.
    ReadBlock(blockID uint64) (data []byte, err error)

    // WriteBlock writes an entire block.
    WriteBlock(blockID uint64, data []byte) error

    // GetBlockChain returns the ordered list of blocks for an inode.
    // For sparse files, returns 0 for hole blocks.
    GetBlockChain(ino uint64) (blockIDs []uint64, err error)

    // SetBlockChain sets the block chain for an inode.
    SetBlockChain(ino uint64, blockIDs []uint64) error
}

// Helper functions for file operations
type BlockChainFile struct {
    store    BlockChainStore
    ino      uint64
    blocks   []uint64
    size     int64
}

func (f *BlockChainFile) ReadAt(p []byte, off int64) (int, error) {
    blockSize := int64(f.store.BlockSize())
    totalRead := 0

    for len(p) > 0 && off < f.size {
        blockIdx := off / blockSize
        blockOff := off % blockSize

        if blockIdx >= int64(len(f.blocks)) {
            break
        }

        blockID := f.blocks[blockIdx]

        // Handle sparse block (hole)
        if blockID == 0 {
            toRead := min(int64(len(p)), blockSize-blockOff)
            // Zero-fill
            for i := int64(0); i < toRead; i++ {
                p[i] = 0
            }
            n := int(toRead)
            p = p[n:]
            off += int64(n)
            totalRead += n
            continue
        }

        // Read from actual block
        data, err := f.store.ReadBlock(blockID)
        if err != nil {
            return totalRead, err
        }

        n := copy(p, data[blockOff:])
        p = p[n:]
        off += int64(n)
        totalRead += n
    }

    if totalRead == 0 && off >= f.size {
        return 0, io.EOF
    }
    return totalRead, nil
}

func (f *BlockChainFile) WriteAt(p []byte, off int64) (int, error) {
    blockSize := int64(f.store.BlockSize())
    totalWritten := 0

    for len(p) > 0 {
        blockIdx := off / blockSize
        blockOff := off % blockSize

        // Extend block chain if necessary
        for int64(len(f.blocks)) <= blockIdx {
            f.blocks = append(f.blocks, 0) // sparse by default
        }

        blockID := f.blocks[blockIdx]

        // Allocate block if this is a hole
        if blockID == 0 {
            var err error
            blockID, err = f.store.AllocBlock()
            if err != nil {
                return totalWritten, err
            }
            f.blocks[blockIdx] = blockID
        }

        // Read existing block data
        data, err := f.store.ReadBlock(blockID)
        if err != nil {
            return totalWritten, err
        }

        // Modify portion
        n := copy(data[blockOff:], p)

        // Write back
        err = f.store.WriteBlock(blockID, data)
        if err != nil {
            return totalWritten, err
        }

        p = p[n:]
        off += int64(n)
        totalWritten += n
    }

    // Update block chain
    err := f.store.SetBlockChain(f.ino, f.blocks)
    if err != nil {
        return totalWritten, err
    }

    return totalWritten, nil
}

// memfs implementation with block pool
type memBlockChainStore struct {
    blockSize  int
    mu         sync.RWMutex
    nextID     uint64
    blocks     map[uint64][]byte
    chains     map[uint64][]uint64  // ino -> block IDs
}

// boltfs implementation
type boltBlockChainStore struct {
    db         *bolt.DB
    bucket     string
    blockSize  int
}
```

**Pros:**
- Sparse file support (holes = block ID 0)
- Incremental updates (modify only changed blocks)
- Predictable memory usage
- Classic, well-understood model
- Block-level deduplication possible (if block IDs reference shared pool)

**Cons:**
- Fixed block size may waste space (small files) or cause fragmentation (large files)
- Metadata overhead for block chain storage
- More complex than byte-oriented
- Read/write requires block chain traversal
- Internal fragmentation (partial last block)

**Best For:**
- Disk-backed filesystems
- Large files with random access
- Sparse files
- Systems needing incremental updates

### Option C: Extent-Based Storage (Modern Filesystem Style)

**Philosophy:** Store ranges of contiguous blocks as extents. Best of both worlds - efficiency of contiguous allocation with flexibility of block-based storage.

```go
package inode

// Extent represents a contiguous range of blocks.
type Extent struct {
    Logical  uint64  // Logical offset in file (in blocks)
    Physical uint64  // Physical block ID (where data starts)
    Length   uint64  // Number of contiguous blocks
}

// ExtentList is a sorted list of extents for a file.
type ExtentList []Extent

// ExtentStore provides extent-based block storage.
// Files are sequences of extents (contiguous block ranges).
type ExtentStore interface {
    // BlockSize returns the block size for this store.
    BlockSize() int

    // AllocExtent allocates a contiguous range of blocks.
    // Returns the physical block ID of the start and actual length allocated.
    // May allocate less than requested if contiguous space unavailable.
    AllocExtent(desiredLength uint64) (physical uint64, length uint64, err error)

    // FreeExtent releases a contiguous range of blocks.
    FreeExtent(physical uint64, length uint64) error

    // ReadExtent reads data from a physical extent.
    ReadExtent(physical uint64, length uint64) (data []byte, err error)

    // WriteExtent writes data to a physical extent.
    // Data length must match extent length * blockSize.
    WriteExtent(physical uint64, data []byte) error

    // GetExtents returns the extent list for an inode.
    GetExtents(ino uint64) (ExtentList, error)

    // SetExtents updates the extent list for an inode.
    SetExtents(ino uint64, extents ExtentList) error
}

// Helper functions for extent operations
func (el ExtentList) Len() int { return len(el) }
func (el ExtentList) Less(i, j int) bool { return el[i].Logical < el[j].Logical }
func (el ExtentList) Swap(i, j int) { el[i], el[j] = el[j], el[i] }

// FindExtent returns the extent and offset within extent for a logical position.
func (el ExtentList) FindExtent(logicalBlock uint64) (extIdx int, offset uint64, found bool) {
    for i, ext := range el {
        if logicalBlock >= ext.Logical && logicalBlock < ext.Logical+ext.Length {
            return i, logicalBlock - ext.Logical, true
        }
    }
    return 0, 0, false
}

// ExtentFile provides file operations on extent-based storage.
type ExtentFile struct {
    store    ExtentStore
    ino      uint64
    extents  ExtentList
    size     int64
}

func (f *ExtentFile) ReadAt(p []byte, off int64) (int, error) {
    blockSize := int64(f.store.BlockSize())
    totalRead := 0

    logicalBlock := uint64(off / blockSize)
    blockOffset := off % blockSize

    for len(p) > 0 && off < f.size {
        // Find extent containing this logical block
        extIdx, offset, found := f.extents.FindExtent(logicalBlock)

        if !found {
            // Sparse region (hole) - return zeros
            toRead := min(int64(len(p)), blockSize-blockOffset)
            for i := int64(0); i < toRead; i++ {
                p[i] = 0
            }
            n := int(toRead)
            p = p[n:]
            off += int64(n)
            totalRead += n
            logicalBlock++
            blockOffset = 0
            continue
        }

        // Read from extent
        ext := f.extents[extIdx]
        blocksAvail := ext.Length - offset
        physicalBlock := ext.Physical + offset

        // Read the extent data (or portion of it)
        data, err := f.store.ReadExtent(physicalBlock, blocksAvail)
        if err != nil {
            return totalRead, err
        }

        n := copy(p, data[blockOffset:])
        p = p[n:]
        off += int64(n)
        totalRead += n

        // Move to next logical block
        logicalBlock = ext.Logical + ext.Length
        blockOffset = 0
    }

    if totalRead == 0 && off >= f.size {
        return 0, io.EOF
    }
    return totalRead, nil
}

func (f *ExtentFile) WriteAt(p []byte, off int64) (int, error) {
    // Writing to extent-based storage:
    // 1. Find affected extents
    // 2. For holes, allocate new extents
    // 3. For existing extents, may need to split/merge
    // 4. COW option: allocate new extent, update pointers atomically

    // Simplified implementation (full implementation would handle splits/merges)
    blockSize := int64(f.store.BlockSize())
    logicalBlock := uint64(off / blockSize)

    // Calculate how many blocks needed
    endOff := off + int64(len(p))
    endBlock := uint64((endOff + blockSize - 1) / blockSize)
    blocksNeeded := endBlock - logicalBlock

    // Find or allocate extent
    extIdx, _, found := f.extents.FindExtent(logicalBlock)

    if !found {
        // Allocate new extent for this region
        physical, length, err := f.store.AllocExtent(blocksNeeded)
        if err != nil {
            return 0, err
        }

        newExt := Extent{
            Logical:  logicalBlock,
            Physical: physical,
            Length:   length,
        }

        f.extents = append(f.extents, newExt)
        sort.Sort(f.extents)
    }

    // Write data to extent
    // (Simplified - real implementation would handle extent spanning)
    // ...

    return len(p), f.store.SetExtents(f.ino, f.extents)
}

// Copy-on-Write support
type COWExtentStore struct {
    ExtentStore
    refCounts sync.Map  // map[uint64]*atomic.Uint64  // physical block -> ref count
}

func (s *COWExtentStore) CloneExtents(srcIno, dstIno uint64) error {
    extents, err := s.GetExtents(srcIno)
    if err != nil {
        return err
    }

    // Increment reference counts for all extents
    for _, ext := range extents {
        key := ext.Physical
        val, _ := s.refCounts.LoadOrStore(key, &atomic.Uint64{})
        ref := val.(*atomic.Uint64)
        ref.Add(1)
    }

    // Destination gets same extent list
    return s.SetExtents(dstIno, extents)
}

func (s *COWExtentStore) WriteExtentCOW(ino uint64, logicalBlock uint64, data []byte) error {
    extents, err := s.GetExtents(ino)
    if err != nil {
        return err
    }

    extIdx, offset, found := ExtentList(extents).FindExtent(logicalBlock)
    if !found {
        return errors.New("extent not found")
    }

    ext := extents[extIdx]

    // Check reference count
    key := ext.Physical
    val, _ := s.refCounts.LoadOrStore(key, &atomic.Uint64{})
    ref := val.(*atomic.Uint64)

    if ref.Load() > 1 {
        // Copy-on-write: allocate new extent
        newPhysical, _, err := s.AllocExtent(1)
        if err != nil {
            return err
        }

        // Copy old data
        oldData, err := s.ReadExtent(ext.Physical+offset, 1)
        if err != nil {
            return err
        }

        // Write new data
        copy(oldData, data)
        err = s.WriteExtent(newPhysical, oldData)
        if err != nil {
            return err
        }

        // Update extent to point to new physical block
        ref.Add(^uint64(0))  // decrement
        extents[extIdx].Physical = newPhysical

        newRef := &atomic.Uint64{}
        newRef.Store(1)
        s.refCounts.Store(newPhysical, newRef)

        return s.SetExtents(ino, extents)
    }

    // Not shared, write directly
    return s.WriteExtent(ext.Physical+offset, data)
}
```

**Pros:**
- Efficient for contiguous allocation (like large sequential files)
- Supports sparse files (gaps in extent list)
- Metadata overhead proportional to fragmentation, not file size
- Natural fit for copy-on-write (clone extent list, share physical blocks)
- Scales well for large files
- Modern filesystem approach

**Cons:**
- Most complex to implement
- Extent management (splitting, merging) is non-trivial
- Random writes may cause fragmentation
- Requires sophisticated allocator for good contiguous allocation

**Best For:**
- Modern disk-based filesystems
- Large sequential files
- Systems with snapshot/clone requirements
- Performance-critical applications

## 5. Atomic Operations and Lock-Free Design

### 5.1 Go's sync/atomic Capabilities

Based on research, Go's `sync/atomic` package provides:

**atomic.Pointer[T]** (Go 1.19+):
- Type-safe atomic pointer operations
- Perfect for copy-on-write data structures
- Enables lock-free reads with occasional atomic swaps

**atomic.Uint64, atomic.Int64**:
- Lock-free counters and flags
- Useful for reference counts, size tracking, allocation

**CompareAndSwap (CAS)**:
- Foundation for lock-free algorithms
- Enables optimistic concurrency

**Memory Ordering**:
- Go's atomics enforce sequential consistency by default
- Strongest guarantee: all threads see operations in same order
- Full memory barriers before and after each atomic operation

**Source:** [Go sync/atomic package](https://pkg.go.dev/sync/atomic), [Atomic Operations in Go](https://medium.com/@hatronix/go-go-lockless-techniques-for-managing-state-and-synchronization-5398370c379b)

### 5.2 Lock-Free BlockStore Patterns

**Copy-on-Write with atomic.Pointer:**

```go
// Extent list stored via atomic pointer for lock-free reads
type LockFreeExtentFile struct {
    store    ExtentStore
    ino      uint64
    extents  atomic.Pointer[ExtentList]  // COW - atomically swap on updates
    size     atomic.Int64
}

func (f *LockFreeExtentFile) ReadAt(p []byte, off int64) (int, error) {
    // Lock-free read - load current extent list
    extents := f.extents.Load()
    if extents == nil {
        return 0, io.EOF
    }

    // Read using snapshot (no locking needed)
    // ... perform read using *extents ...
    return n, nil
}

func (f *LockFreeExtentFile) WriteAt(p []byte, off int64) (int, error) {
    // Copy-on-write pattern
    for {
        // Load current extent list
        oldExtents := f.extents.Load()

        // Create modified copy
        newExtents := make(ExtentList, len(*oldExtents))
        copy(newExtents, *oldExtents)

        // Modify the copy
        // ... add/modify extents in newExtents ...

        // Atomically swap if unchanged
        if f.extents.CompareAndSwap(oldExtents, &newExtents) {
            f.size.Store(newSize)
            return len(p), nil
        }
        // CAS failed, retry with new snapshot
    }
}
```

**Reference Counting:**

```go
type BlockRefCounts struct {
    counts sync.Map  // map[uint64]*atomic.Uint64
}

func (r *BlockRefCounts) Inc(blockID uint64) {
    val, _ := r.counts.LoadOrStore(blockID, &atomic.Uint64{})
    ref := val.(*atomic.Uint64)
    ref.Add(1)
}

func (r *BlockRefCounts) Dec(blockID uint64) uint64 {
    val, ok := r.counts.Load(blockID)
    if !ok {
        return 0
    }
    ref := val.(*atomic.Uint64)
    newCount := ref.Add(^uint64(0))  // decrement by 1
    return newCount
}
```

### 5.3 When Locks Are Required

**Atomics work well for:**
- Single-value updates (pointers, counters, flags)
- Read-heavy workloads with occasional updates
- Copy-on-write patterns

**Locks required for:**
- Multi-step operations requiring consistency
- Complex invariants across multiple values
- Block allocation (need to coordinate free list)
- Extent merging/splitting (complex structural changes)

**Recommendation:** Use atomic.Pointer for file extent lists (enables lock-free reads), but use mutexes for the underlying block allocator and complex extent operations.

## 6. Integration with Inode Package

### 6.1 Where BlockStore Fits

**Option 1: FileSystem Holds BlockStore**

```go
// Filesystem owns the block store
type FileSystem struct {
    root   *Inode
    blocks BlockStore
    // ...
}

// Inodes just reference data by ID
type Inode struct {
    Ino  uint64
    Size atomic.Int64
    // No direct data reference
}

// File operations go through filesystem
func (fs *FileSystem) OpenFile(path string) (*File, error) {
    inode := // ... resolve path ...
    return &File{
        inode:  inode,
        blocks: fs.blocks,  // Pass block store to file
    }, nil
}
```

**Pros:**
- Single block store instance for entire filesystem
- Easier to implement block-level deduplication
- Centralized allocation management
- Natural fit for disk-backed systems

**Cons:**
- File operations need filesystem reference
- Tighter coupling between inode and filesystem

**Option 2: Inode Holds BlockStore Reference**

```go
type Inode struct {
    Ino    uint64
    Size   atomic.Int64
    Blocks BlockStore  // Each inode has block store reference
    // ...
}
```

**Pros:**
- Inode more self-contained
- Simpler file operations (just need inode)

**Cons:**
- Every inode references same store (memory overhead)
- Harder to enforce single allocator instance

**Option 3: Separate File Abstraction**

```go
// Current inode package stays metadata-only
type Inode struct {
    Ino  uint64
    Size atomic.Int64
    // No data storage
}

// New package for file data
package filedata

type File struct {
    Inode  *inode.Inode
    Blocks BlockStore
}
```

**Pros:**
- Clean separation: inode = metadata, file = data
- Inode package remains simple and focused
- Flexibility in data storage strategy

**Cons:**
- Additional abstraction layer
- More packages to coordinate

### 6.2 Recommended Architecture

**Recommendation: Option 1 (FileSystem Holds BlockStore)**

This matches the current absfs architecture where the FileSystem is responsible for all storage:

```go
// In memfs package
type FileSystem struct {
    root   *inode.Inode
    blocks ByteStore  // or BlockChainStore or ExtentStore
    // ... existing fields ...
}

// In boltfs package
type FileSystem struct {
    db     *bolt.DB
    root   uint64
    blocks ExtentStore  // Extent-based for better scalability
    // ... existing fields ...
}
```

### 6.3 Migration Path

**Phase 1: Introduce interface**
- Add BlockStore interface to inode package
- Keep as optional - filesystems can choose to implement or not

**Phase 2: Refactor memfs**
- Extract current data storage into ByteStore implementation
- Keep backward compatibility

**Phase 3: Enhance boltfs**
- Implement ExtentStore for better large file performance
- Add COW support for snapshots

**Phase 4: New implementations**
- Enable disk-backed filesystem using ExtentStore
- Support advanced features (snapshots, deduplication)

## 7. Recommendations

### 7.1 Start with Option A (ByteStore)

**Recommendation:** Begin with the simplest approach - ByteStore interface.

**Rationale:**
1. Matches current memfs/boltfs architecture
2. Easy migration path (formalize existing pattern)
3. Sufficient for most current use cases
4. Can evolve to more complex stores later

**Implementation:**

```go
// inode/blockstore.go
package inode

// ByteStore provides simple byte-oriented file storage.
type ByteStore interface {
    ReadAt(ino uint64, p []byte, off int64) (n int, err error)
    WriteAt(ino uint64, p []byte, off int64) (n int, err error)
    Truncate(ino uint64, size int64) error
    Remove(ino uint64) error
    Stat(ino uint64) (size int64, err error)
}
```

### 7.2 Future Path to Extents

**When to migrate to ExtentStore:**
- Large file support required (multi-GB files)
- Sparse file support needed
- Snapshot/clone features desired
- Disk-backed filesystem implementation

**Migration approach:**
- Add ExtentStore interface alongside ByteStore
- Implement ExtentStore wrapper around ByteStore for compatibility
- New filesystems can choose which to implement

### 7.3 Lock-Free Recommendations

**Use atomic operations for:**
1. File size tracking (`atomic.Int64`)
2. Extent list COW (`atomic.Pointer[ExtentList]`)
3. Reference counts for COW blocks (`atomic.Uint64`)

**Use mutexes for:**
1. Block allocation
2. Extent splitting/merging
3. Directory operations (already done)

**Pattern:**

```go
type File struct {
    ino      uint64
    extents  atomic.Pointer[ExtentList]  // Lock-free reads
    size     atomic.Int64                 // Lock-free size reads

    writeMu  sync.Mutex  // Serialize writes (complex operations)
}

// Lock-free read path
func (f *File) Read(p []byte) (int, error) {
    extents := f.extents.Load()  // No lock needed
    size := f.size.Load()
    // ... read using snapshot ...
}

// Locked write path (for complex extent modifications)
func (f *File) Write(p []byte) (int, error) {
    f.writeMu.Lock()
    defer f.writeMu.Unlock()

    // Complex extent operations under lock
    // Then atomically update extent pointer
    f.extents.Store(newExtents)
    f.size.Store(newSize)
}
```

## 8. Summary

### Recommended Approach

1. **Phase 1**: Implement Option A (ByteStore)
   - Simple interface matching current architecture
   - Easy to implement for memfs and boltfs
   - Sufficient for current needs

2. **Phase 2**: Add Option C (ExtentStore) as advanced option
   - For large files and sparse file support
   - Enable COW/snapshot features
   - Disk-backed filesystem implementations

3. **Lock-Free Strategy**:
   - Use `atomic.Pointer` for extent lists (lock-free reads)
   - Use `atomic.Int64` for size tracking
   - Use mutexes for complex modifications
   - Prefer simplicity over premature optimization

4. **Architecture**: FileSystem owns BlockStore
   - Single instance per filesystem
   - Passed to File handles for operations
   - Enables centralized allocation and deduplication

### Next Steps

1. Review this design with stakeholders
2. Implement ByteStore interface in inode package
3. Refactor memfs to use ByteStore (backward compatible)
4. Add ExtentStore interface for future use
5. Document interface usage and implementation examples

## References

- [Ext4 Disk Layout](https://ext4.wiki.kernel.org/index.php/Ext4_Disk_Layout)
- [Extents in Ext4](https://blogs.oracle.com/linux/extents-and-extent-allocation-in-ext4)
- [ZFS Wikipedia](https://en.wikipedia.org/wiki/ZFS)
- [Understanding ZFS Block Sizes](https://ibug.io/blog/2023/10/zfs-block-size/)
- [Go sync/atomic package](https://pkg.go.dev/sync/atomic)
- [Atomic Operations in Go](https://medium.com/@hatronix/go-go-lockless-techniques-for-managing-state-and-synchronization-5398370c379b)
- [Lock-Free Data Structures in Go](https://dev.to/aaravjoshi/mastering-lock-free-data-structures-in-go-ring-buffers-queues-and-performance-optimization-2f2d)
