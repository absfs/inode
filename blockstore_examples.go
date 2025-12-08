package inode

import (
	"errors"
	"io"
	"sort"
	"sync"
	"sync/atomic"
)

// Example implementations of the BlockStore interfaces.
// These demonstrate the patterns and provide reference implementations.

// MemByteStore implements ByteStore using an in-memory map.
//
// This is the simplest implementation, suitable for testing and
// small in-memory filesystems. It stores each file as a single
// contiguous byte slice.
//
// Thread-safe for concurrent use.
type MemByteStore struct {
	mu   sync.RWMutex
	data map[uint64][]byte
}

// NewMemByteStore creates a new in-memory ByteStore.
func NewMemByteStore() *MemByteStore {
	return &MemByteStore{
		data: make(map[uint64][]byte),
	}
}

func (s *MemByteStore) ReadAt(ino uint64, p []byte, off int64) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data := s.data[ino]
	if data == nil {
		return 0, io.EOF
	}

	if off >= int64(len(data)) {
		return 0, io.EOF
	}

	n := copy(p, data[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func (s *MemByteStore) WriteAt(ino uint64, p []byte, off int64) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data := s.data[ino]
	end := off + int64(len(p))

	// Extend if necessary
	if end > int64(len(data)) {
		newData := make([]byte, end)
		copy(newData, data)
		data = newData
		s.data[ino] = data
	}

	return copy(data[off:], p), nil
}

func (s *MemByteStore) Truncate(ino uint64, size int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data := s.data[ino]

	if size == 0 {
		s.data[ino] = nil
		return nil
	}

	if int64(len(data)) == size {
		return nil
	}

	if size < int64(len(data)) {
		s.data[ino] = data[:size]
		return nil
	}

	// Extend with zeros
	newData := make([]byte, size)
	copy(newData, data)
	s.data[ino] = newData
	return nil
}

func (s *MemByteStore) Remove(ino uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.data, ino)
	return nil
}

func (s *MemByteStore) Stat(ino uint64) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data := s.data[ino]
	return int64(len(data)), nil
}

// MemBlockChainStore implements BlockChainStore using in-memory storage.
//
// Files are represented as chains of fixed-size blocks. This demonstrates
// the block chain pattern with support for sparse files.
//
// Thread-safe for concurrent use.
type MemBlockChainStore struct {
	blockSize int
	mu        sync.RWMutex
	nextID    uint64
	blocks    map[uint64][]byte      // blockID -> data
	chains    map[uint64][]uint64    // ino -> block IDs
}

// NewMemBlockChainStore creates a new in-memory BlockChainStore.
func NewMemBlockChainStore(blockSize int) *MemBlockChainStore {
	return &MemBlockChainStore{
		blockSize: blockSize,
		nextID:    1, // 0 is reserved for holes
		blocks:    make(map[uint64][]byte),
		chains:    make(map[uint64][]uint64),
	}
}

func (s *MemBlockChainStore) BlockSize() int {
	return s.blockSize
}

func (s *MemBlockChainStore) AllocBlock() (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := s.nextID
	s.nextID++
	s.blocks[id] = make([]byte, s.blockSize)
	return id, nil
}

func (s *MemBlockChainStore) FreeBlock(blockID uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.blocks, blockID)
	return nil
}

func (s *MemBlockChainStore) ReadBlock(blockID uint64) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data := s.blocks[blockID]
	if data == nil {
		return make([]byte, s.blockSize), nil // undefined data (return zeros)
	}

	result := make([]byte, s.blockSize)
	copy(result, data)
	return result, nil
}

func (s *MemBlockChainStore) WriteBlock(blockID uint64, data []byte) error {
	if len(data) != s.blockSize {
		return errors.New("data length must equal block size")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.blocks[blockID] == nil {
		return errors.New("block not allocated")
	}

	blockData := make([]byte, s.blockSize)
	copy(blockData, data)
	s.blocks[blockID] = blockData
	return nil
}

func (s *MemBlockChainStore) GetBlockChain(ino uint64) ([]uint64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	chain := s.chains[ino]
	if chain == nil {
		return []uint64{}, nil
	}

	result := make([]uint64, len(chain))
	copy(result, chain)
	return result, nil
}

func (s *MemBlockChainStore) SetBlockChain(ino uint64, blockIDs []uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if blockIDs == nil {
		delete(s.chains, ino)
		return nil
	}

	chain := make([]uint64, len(blockIDs))
	copy(chain, blockIDs)
	s.chains[ino] = chain
	return nil
}

// MemExtentStore implements ExtentStore using in-memory storage.
//
// Files are represented as lists of extents. This demonstrates the
// extent-based pattern with support for sparse files and efficient
// contiguous allocation.
//
// Thread-safe for concurrent use.
type MemExtentStore struct {
	blockSize int
	mu        sync.RWMutex
	nextID    uint64
	blocks    map[uint64][]byte      // physical blockID -> data
	extents   map[uint64]ExtentList  // ino -> extents
}

// NewMemExtentStore creates a new in-memory ExtentStore.
func NewMemExtentStore(blockSize int) *MemExtentStore {
	return &MemExtentStore{
		blockSize: blockSize,
		nextID:    1,
		blocks:    make(map[uint64][]byte),
		extents:   make(map[uint64]ExtentList),
	}
}

func (s *MemExtentStore) BlockSize() int {
	return s.blockSize
}

func (s *MemExtentStore) AllocExtent(desiredLength uint64) (uint64, uint64, error) {
	if desiredLength == 0 {
		return 0, 0, errors.New("cannot allocate zero-length extent")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Simple allocator: allocate contiguous IDs
	physical := s.nextID
	s.nextID += desiredLength

	// Allocate blocks
	for i := uint64(0); i < desiredLength; i++ {
		s.blocks[physical+i] = make([]byte, s.blockSize)
	}

	return physical, desiredLength, nil
}

func (s *MemExtentStore) FreeExtent(physical uint64, length uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := uint64(0); i < length; i++ {
		delete(s.blocks, physical+i)
	}
	return nil
}

func (s *MemExtentStore) ReadExtent(physical uint64, length uint64) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]byte, int(length)*s.blockSize)

	for i := uint64(0); i < length; i++ {
		blockData := s.blocks[physical+i]
		if blockData != nil {
			copy(result[i*uint64(s.blockSize):], blockData)
		}
		// else: undefined data (leave as zeros)
	}

	return result, nil
}

func (s *MemExtentStore) WriteExtent(physical uint64, length uint64, data []byte) error {
	expectedLen := int(length) * s.blockSize
	if len(data) != expectedLen {
		return errors.New("data length must equal length * blockSize")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for i := uint64(0); i < length; i++ {
		if s.blocks[physical+i] == nil {
			return errors.New("extent not allocated")
		}

		blockData := make([]byte, s.blockSize)
		copy(blockData, data[i*uint64(s.blockSize):])
		s.blocks[physical+i] = blockData
	}

	return nil
}

func (s *MemExtentStore) GetExtents(ino uint64) (ExtentList, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	extents := s.extents[ino]
	if extents == nil {
		return ExtentList{}, nil
	}

	result := make(ExtentList, len(extents))
	copy(result, extents)
	return result, nil
}

func (s *MemExtentStore) SetExtents(ino uint64, extents ExtentList) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if extents == nil || len(extents) == 0 {
		delete(s.extents, ino)
		return nil
	}

	// Validate and copy
	result := make(ExtentList, len(extents))
	copy(result, extents)

	// Ensure sorted
	if !sort.IsSorted(result) {
		sort.Sort(result)
	}

	s.extents[ino] = result
	return nil
}

// COWMemExtentStore extends MemExtentStore with copy-on-write support.
//
// Implements the COWStore interface to enable efficient cloning and snapshots.
type COWMemExtentStore struct {
	*MemExtentStore
	refCounts sync.Map // map[uint64]*atomic.Uint64
}

// NewCOWMemExtentStore creates a new COW-enabled ExtentStore.
func NewCOWMemExtentStore(blockSize int) *COWMemExtentStore {
	return &COWMemExtentStore{
		MemExtentStore: NewMemExtentStore(blockSize),
	}
}

func (s *COWMemExtentStore) CloneExtents(srcIno, dstIno uint64) error {
	s.mu.RLock()
	extents := s.extents[srcIno]
	s.mu.RUnlock()

	if extents == nil {
		return nil
	}

	// Increment reference counts for all physical blocks
	for _, ext := range extents {
		for i := uint64(0); i < ext.Length; i++ {
			physical := ext.Physical + i
			val, _ := s.refCounts.LoadOrStore(physical, &atomic.Uint64{})
			ref := val.(*atomic.Uint64)
			ref.Add(1)
		}
	}

	// Clone extent list
	cloned := make(ExtentList, len(extents))
	copy(cloned, extents)

	s.mu.Lock()
	s.extents[dstIno] = cloned
	s.mu.Unlock()

	return nil
}

func (s *COWMemExtentStore) BreakCOW(ino uint64, logicalBlock uint64) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	extents := s.extents[ino]
	if extents == nil {
		return 0, errors.New("inode not found")
	}

	extIdx, offset, found := extents.FindExtent(logicalBlock)
	if !found {
		return 0, errors.New("block not in extent")
	}

	ext := extents[extIdx]
	physical := ext.Physical + offset

	// Check reference count
	val, ok := s.refCounts.Load(physical)
	if !ok {
		// Not shared, return as-is
		return physical, nil
	}

	ref := val.(*atomic.Uint64)
	if ref.Load() <= 1 {
		// Not shared, return as-is
		return physical, nil
	}

	// Need to copy
	newPhysical := s.nextID
	s.nextID++

	// Copy block data
	s.blocks[newPhysical] = make([]byte, s.blockSize)
	copy(s.blocks[newPhysical], s.blocks[physical])

	// Update extent to point to new block
	// If this breaks an extent in the middle, we need to split it
	if offset > 0 || ext.Length > 1 {
		// Complex case: split extent
		// For simplicity, just handle the single-block case
		if ext.Length == 1 {
			extents[extIdx].Physical = newPhysical
		} else {
			// Would need to split extent into up to 3 parts
			// This is left as an exercise
			return 0, errors.New("extent splitting not implemented in example")
		}
	} else {
		extents[extIdx].Physical = newPhysical
	}

	// Decrement old reference, set new reference to 1
	ref.Add(^uint64(0)) // decrement by 1
	newRef := &atomic.Uint64{}
	newRef.Store(1)
	s.refCounts.Store(newPhysical, newRef)

	s.extents[ino] = extents

	return newPhysical, nil
}

func (s *COWMemExtentStore) RefCount(physical uint64) uint64 {
	val, ok := s.refCounts.Load(physical)
	if !ok {
		return 0
	}
	ref := val.(*atomic.Uint64)
	return ref.Load()
}
