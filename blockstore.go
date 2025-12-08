// Package inode provides block storage abstractions for filesystem implementations.
//
// This file defines the BlockStore interfaces that enable different storage strategies
// ranging from simple byte arrays to sophisticated extent-based allocation.
package inode

// ByteStore provides simple byte-oriented file storage.
//
// Each inode has a single variable-sized byte array. This is the simplest
// storage model, best suited for in-memory filesystems or small files.
//
// Implementations should be safe for concurrent use by multiple goroutines.
//
// Example usage:
//
//	store := NewMemByteStore()
//	store.WriteAt(ino, []byte("hello"), 0)
//	data := make([]byte, 5)
//	n, err := store.ReadAt(ino, data, 0)
type ByteStore interface {
	// ReadAt reads len(p) bytes from the file at the given offset.
	// Returns the number of bytes read and any error encountered.
	// Returns io.EOF when offset is at or beyond the end of file.
	//
	// The behavior matches io.ReaderAt: a Read that encounters EOF
	// may return either (n, io.EOF) or (n, nil) and a subsequent
	// Read will return (0, io.EOF).
	ReadAt(ino uint64, p []byte, off int64) (n int, err error)

	// WriteAt writes len(p) bytes to the file at the given offset.
	// Extends the file if necessary, filling any gaps with zeros.
	// Returns the number of bytes written and any error encountered.
	//
	// WriteAt should not affect nor be affected by the underlying data
	// representation - it should handle sparse files efficiently.
	WriteAt(ino uint64, p []byte, off int64) (n int, err error)

	// Truncate changes the file size.
	// If the file is larger than size, it is truncated to size.
	// If the file is smaller, it is extended with zero bytes to size.
	Truncate(ino uint64, size int64) error

	// Remove deletes all data associated with the inode.
	// After Remove, operations on this inode should return an error.
	// Calling Remove on a non-existent inode should not return an error.
	Remove(ino uint64) error

	// Stat returns the current size of the file in bytes.
	// Returns (0, nil) for empty or non-existent files.
	Stat(ino uint64) (size int64, err error)
}

// BlockSize represents a fixed block size in bytes.
type BlockSize int

const (
	// BlockSize4K is the standard 4 KiB block size used by most filesystems.
	BlockSize4K BlockSize = 4096

	// BlockSize8K is used by some filesystems for better performance.
	BlockSize8K BlockSize = 8192

	// BlockSize16K is used by filesystems optimized for larger files.
	BlockSize16K BlockSize = 16384

	// BlockSize64K is used for very large files or specific workloads.
	BlockSize64K BlockSize = 65536
)

// BlockChainStore provides fixed-size block storage with chaining.
//
// Files are represented as sequences of fixed-size blocks, potentially
// with gaps (sparse files). This model enables incremental updates and
// efficient handling of large files with random access patterns.
//
// Block IDs are opaque uint64 values. A block ID of 0 represents a hole
// (unallocated block) in sparse files.
//
// Implementations should be safe for concurrent use by multiple goroutines.
type BlockChainStore interface {
	// BlockSize returns the fixed block size used by this store.
	BlockSize() int

	// AllocBlock allocates a new block and returns its unique ID.
	// The block's initial contents are undefined (may be zeros or garbage).
	AllocBlock() (blockID uint64, err error)

	// FreeBlock releases a block for reuse.
	// The block's contents become undefined after freeing.
	// Freeing a block that is already free should not return an error.
	FreeBlock(blockID uint64) error

	// ReadBlock reads the entire contents of a block.
	// Returns a byte slice of length BlockSize().
	// Reading a freed or never-allocated block returns undefined data.
	ReadBlock(blockID uint64) (data []byte, err error)

	// WriteBlock writes the entire contents of a block.
	// The data length must equal BlockSize().
	// Writing to a freed block is an error.
	WriteBlock(blockID uint64, data []byte) error

	// GetBlockChain returns the ordered list of block IDs for an inode.
	// For sparse files, unallocated blocks are represented as ID 0.
	// An empty or non-existent file returns an empty slice.
	GetBlockChain(ino uint64) (blockIDs []uint64, err error)

	// SetBlockChain updates the block chain for an inode.
	// This atomically replaces the entire block chain.
	// Block IDs of 0 represent holes (sparse regions).
	SetBlockChain(ino uint64, blockIDs []uint64) error
}

// Extent represents a contiguous range of blocks.
//
// An extent maps a logical range in a file to a physical range in storage.
// This enables efficient representation of large contiguous allocations.
type Extent struct {
	// Logical is the starting logical block number in the file.
	// For a file with 4 KiB blocks, logical block 0 is bytes 0-4095,
	// logical block 1 is bytes 4096-8191, etc.
	Logical uint64

	// Physical is the physical block ID where this extent's data is stored.
	// This is opaque to the caller - the ExtentStore manages the mapping.
	Physical uint64

	// Length is the number of contiguous blocks in this extent.
	// An extent maps blocks [Logical, Logical+Length) to
	// physical blocks [Physical, Physical+Length).
	Length uint64
}

// ExtentList is a list of extents for a file.
// Extents should be sorted by Logical block number with no overlaps.
// Gaps between extents represent sparse regions (holes).
type ExtentList []Extent

// ExtentStore provides extent-based block storage.
//
// Files are represented as lists of extents - contiguous ranges of blocks.
// This is the most sophisticated storage model, providing excellent
// performance for large sequential files while supporting sparse files
// and enabling copy-on-write semantics.
//
// Implementations should be safe for concurrent use by multiple goroutines.
type ExtentStore interface {
	// BlockSize returns the fixed block size used by this store.
	BlockSize() int

	// AllocExtent allocates a contiguous range of blocks.
	// Returns the physical block ID of the start and the actual length allocated.
	// May allocate less than requested if contiguous space is unavailable.
	// If any blocks are allocated, returns the extent and nil error.
	// If no blocks can be allocated, returns zero extent and an error.
	AllocExtent(desiredLength uint64) (physical uint64, length uint64, err error)

	// FreeExtent releases a contiguous range of blocks for reuse.
	// The blocks' contents become undefined after freeing.
	// All blocks [physical, physical+length) must be currently allocated.
	FreeExtent(physical uint64, length uint64) error

	// ReadExtent reads data from a physical extent.
	// Returns a byte slice of length (length * BlockSize()).
	// Reading from freed or never-allocated blocks returns undefined data.
	ReadExtent(physical uint64, length uint64) (data []byte, err error)

	// WriteExtent writes data to a physical extent.
	// The data length must equal (length * BlockSize()).
	// All blocks in the range must be currently allocated.
	WriteExtent(physical uint64, length uint64, data []byte) error

	// GetExtents returns the extent list for an inode.
	// Extents are sorted by logical block number.
	// Gaps between extents represent sparse regions.
	// An empty or non-existent file returns an empty extent list.
	GetExtents(ino uint64) (ExtentList, error)

	// SetExtents atomically updates the extent list for an inode.
	// This replaces the entire extent list.
	// Extents should be sorted by Logical block number with no overlaps.
	SetExtents(ino uint64, extents ExtentList) error
}

// Helper functions for ExtentList

// Len returns the number of extents in the list.
func (el ExtentList) Len() int { return len(el) }

// Less reports whether the extent at index i should sort before extent j.
func (el ExtentList) Less(i, j int) bool { return el[i].Logical < el[j].Logical }

// Swap exchanges the extents at indices i and j.
func (el ExtentList) Swap(i, j int) { el[i], el[j] = el[j], el[i] }

// FindExtent locates the extent containing the given logical block.
//
// Returns:
//   - extIdx: the index of the extent in the list
//   - offset: the offset within the extent (logicalBlock - extent.Logical)
//   - found: true if the block is within an allocated extent, false if it's in a hole
//
// Example:
//
//	extents := ExtentList{
//	    {Logical: 0, Physical: 100, Length: 10},   // blocks 0-9
//	    {Logical: 20, Physical: 200, Length: 5},   // blocks 20-24
//	}
//	// Block 5 is in first extent at offset 5
//	extIdx, offset, found := extents.FindExtent(5)  // returns (0, 5, true)
//	// Block 15 is in a hole
//	extIdx, offset, found := extents.FindExtent(15) // returns (0, 0, false)
func (el ExtentList) FindExtent(logicalBlock uint64) (extIdx int, offset uint64, found bool) {
	for i, ext := range el {
		if logicalBlock >= ext.Logical && logicalBlock < ext.Logical+ext.Length {
			return i, logicalBlock - ext.Logical, true
		}
	}
	return 0, 0, false
}

// TotalBlocks returns the total number of allocated blocks across all extents.
//
// This does not include sparse regions (holes). To get the total file size
// in blocks, use the last extent's Logical + Length.
func (el ExtentList) TotalBlocks() uint64 {
	var total uint64
	for _, ext := range el {
		total += ext.Length
	}
	return total
}

// LogicalSize returns the logical size of the file in blocks.
//
// This is the highest logical block number that could be accessed,
// including any sparse regions. For an empty extent list, returns 0.
//
// Example:
//
//	extents := ExtentList{
//	    {Logical: 0, Physical: 100, Length: 10},
//	    {Logical: 20, Physical: 200, Length: 5},
//	}
//	size := extents.LogicalSize()  // returns 25 (blocks 0-24)
func (el ExtentList) LogicalSize() uint64 {
	if len(el) == 0 {
		return 0
	}
	last := el[len(el)-1]
	return last.Logical + last.Length
}

// COWStore extends a BlockStore with copy-on-write capabilities.
//
// Copy-on-write allows multiple inodes to share the same physical blocks
// until one of them modifies the data. This enables efficient cloning
// and snapshotting.
//
// Implementations must track reference counts for shared blocks and
// perform copy-before-write when modifying shared data.
type COWStore interface {
	// CloneExtents creates a copy-on-write clone of src's extent list for dst.
	// Both inodes will share the same physical blocks until one modifies them.
	// Reference counts for all shared blocks are incremented.
	CloneExtents(srcIno, dstIno uint64) error

	// BreakCOW ensures that the given extent is not shared.
	// If the extent is shared (refcount > 1), allocates a new extent,
	// copies the data, and updates the inode to use the new extent.
	// If the extent is not shared, does nothing.
	// Returns the (possibly new) physical block ID.
	BreakCOW(ino uint64, logicalBlock uint64) (physical uint64, err error)

	// RefCount returns the reference count for a physical block.
	// Returns 0 if the block is not allocated or not tracked.
	RefCount(physical uint64) uint64
}

// Utility functions

// min returns the smaller of two int64 values.
func min(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

// max returns the larger of two int64 values.
func max(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// divRoundUp divides a by b, rounding up to the nearest integer.
func divRoundUp(a, b int64) int64 {
	return (a + b - 1) / b
}

// BlockCount returns the number of blocks needed to store size bytes.
func BlockCount(size int64, blockSize int) int64 {
	if size == 0 {
		return 0
	}
	return divRoundUp(size, int64(blockSize))
}

// BlockOffset returns the block number and offset within block for a byte offset.
//
// Example with 4096-byte blocks:
//
//	block, offset := BlockOffset(5000, 4096)  // returns (1, 904)
func BlockOffset(byteOffset int64, blockSize int) (block uint64, offset int64) {
	bs := int64(blockSize)
	return uint64(byteOffset / bs), byteOffset % bs
}

// BlockRange returns the range of blocks that contain the byte range [start, start+length).
//
// Returns the first block number and the number of blocks in the range.
//
// Example with 4096-byte blocks:
//
//	first, count := BlockRange(5000, 3000, 4096)  // returns (1, 2)
//	// Covers bytes 5000-7999, which spans blocks 1-2
func BlockRange(start, length int64, blockSize int) (firstBlock uint64, numBlocks uint64) {
	if length == 0 {
		return 0, 0
	}
	bs := int64(blockSize)
	firstBlock = uint64(start / bs)
	lastByte := start + length - 1
	lastBlock := uint64(lastByte / bs)
	numBlocks = lastBlock - firstBlock + 1
	return firstBlock, numBlocks
}

// BlockStoreReader wraps a ByteStore to implement io.ReaderAt.
type BlockStoreReader struct {
	Store ByteStore
	Ino   uint64
}

// ReadAt implements io.ReaderAt.
func (r *BlockStoreReader) ReadAt(p []byte, off int64) (n int, err error) {
	return r.Store.ReadAt(r.Ino, p, off)
}

// BlockStoreWriter wraps a ByteStore to implement io.WriterAt.
type BlockStoreWriter struct {
	Store ByteStore
	Ino   uint64
}

// WriteAt implements io.WriterAt.
func (w *BlockStoreWriter) WriteAt(p []byte, off int64) (n int, err error) {
	return w.Store.WriteAt(w.Ino, p, off)
}

// BlockStoreReadWriter wraps a ByteStore to implement io.ReaderAt and io.WriterAt.
type BlockStoreReadWriter struct {
	Store ByteStore
	Ino   uint64
}

// ReadAt implements io.ReaderAt.
func (rw *BlockStoreReadWriter) ReadAt(p []byte, off int64) (n int, err error) {
	return rw.Store.ReadAt(rw.Ino, p, off)
}

// WriteAt implements io.WriterAt.
func (rw *BlockStoreReadWriter) WriteAt(p []byte, off int64) (n int, err error) {
	return rw.Store.WriteAt(rw.Ino, p, off)
}
