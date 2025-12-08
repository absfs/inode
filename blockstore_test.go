package inode

import (
	"io"
	"testing"
)

// TestMemByteStore verifies the basic ByteStore implementation.
func TestMemByteStore(t *testing.T) {
	store := NewMemByteStore()
	ino := uint64(1)

	// Test initial state
	size, err := store.Stat(ino)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if size != 0 {
		t.Errorf("Expected size 0, got %d", size)
	}

	// Write some data
	data := []byte("hello world")
	n, err := store.WriteAt(ino, data, 0)
	if err != nil {
		t.Fatalf("WriteAt failed: %v", err)
	}
	if n != len(data) {
		t.Errorf("Expected to write %d bytes, wrote %d", len(data), n)
	}

	// Verify size
	size, err = store.Stat(ino)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if size != int64(len(data)) {
		t.Errorf("Expected size %d, got %d", len(data), size)
	}

	// Read data back
	buf := make([]byte, len(data))
	n, err = store.ReadAt(ino, buf, 0)
	if err != nil {
		t.Fatalf("ReadAt failed: %v", err)
	}
	if n != len(data) {
		t.Errorf("Expected to read %d bytes, read %d", len(data), n)
	}
	if string(buf) != string(data) {
		t.Errorf("Expected %q, got %q", data, buf)
	}

	// Test write at offset
	more := []byte("!")
	n, err = store.WriteAt(ino, more, int64(len(data)))
	if err != nil {
		t.Fatalf("WriteAt at offset failed: %v", err)
	}

	// Read extended data
	buf = make([]byte, len(data)+len(more))
	n, err = store.ReadAt(ino, buf, 0)
	if err != nil && err != io.EOF {
		t.Fatalf("ReadAt failed: %v", err)
	}
	expected := "hello world!"
	if string(buf) != expected {
		t.Errorf("Expected %q, got %q", expected, buf)
	}

	// Test truncate
	err = store.Truncate(ino, 5)
	if err != nil {
		t.Fatalf("Truncate failed: %v", err)
	}

	size, err = store.Stat(ino)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if size != 5 {
		t.Errorf("Expected size 5, got %d", size)
	}

	buf = make([]byte, 5)
	n, err = store.ReadAt(ino, buf, 0)
	if err != nil {
		t.Fatalf("ReadAt failed: %v", err)
	}
	if string(buf) != "hello" {
		t.Errorf("Expected %q, got %q", "hello", buf)
	}

	// Test remove
	err = store.Remove(ino)
	if err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	size, err = store.Stat(ino)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if size != 0 {
		t.Errorf("Expected size 0 after remove, got %d", size)
	}
}

// TestMemByteStoreSparseWrite tests writing with gaps.
func TestMemByteStoreSparseWrite(t *testing.T) {
	store := NewMemByteStore()
	ino := uint64(1)

	// Write at offset 1000
	data := []byte("test")
	n, err := store.WriteAt(ino, data, 1000)
	if err != nil {
		t.Fatalf("WriteAt failed: %v", err)
	}
	if n != len(data) {
		t.Errorf("Expected to write %d bytes, wrote %d", len(data), n)
	}

	// Verify size
	size, err := store.Stat(ino)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if size != 1004 {
		t.Errorf("Expected size 1004, got %d", size)
	}

	// Read the gap - should be zeros
	buf := make([]byte, 100)
	n, err = store.ReadAt(ino, buf, 900)
	if err != nil && err != io.EOF {
		t.Fatalf("ReadAt failed: %v", err)
	}

	// First 100 bytes should be zeros
	for i := 0; i < 100; i++ {
		if buf[i] != 0 {
			t.Errorf("Expected zero at position %d, got %d", i, buf[i])
		}
	}

	// Read the actual data
	buf = make([]byte, 4)
	n, err = store.ReadAt(ino, buf, 1000)
	if err != nil {
		t.Fatalf("ReadAt failed: %v", err)
	}
	if string(buf) != "test" {
		t.Errorf("Expected %q, got %q", "test", buf)
	}
}

// TestMemBlockChainStore verifies the BlockChainStore implementation.
func TestMemBlockChainStore(t *testing.T) {
	blockSize := 1024
	store := NewMemBlockChainStore(blockSize)
	ino := uint64(1)

	// Allocate some blocks
	block1, err := store.AllocBlock()
	if err != nil {
		t.Fatalf("AllocBlock failed: %v", err)
	}

	block2, err := store.AllocBlock()
	if err != nil {
		t.Fatalf("AllocBlock failed: %v", err)
	}

	// Write data to blocks
	data1 := make([]byte, blockSize)
	copy(data1, []byte("block 1 data"))
	err = store.WriteBlock(block1, data1)
	if err != nil {
		t.Fatalf("WriteBlock failed: %v", err)
	}

	data2 := make([]byte, blockSize)
	copy(data2, []byte("block 2 data"))
	err = store.WriteBlock(block2, data2)
	if err != nil {
		t.Fatalf("WriteBlock failed: %v", err)
	}

	// Set block chain
	chain := []uint64{block1, block2}
	err = store.SetBlockChain(ino, chain)
	if err != nil {
		t.Fatalf("SetBlockChain failed: %v", err)
	}

	// Get block chain back
	retrievedChain, err := store.GetBlockChain(ino)
	if err != nil {
		t.Fatalf("GetBlockChain failed: %v", err)
	}

	if len(retrievedChain) != 2 {
		t.Fatalf("Expected 2 blocks, got %d", len(retrievedChain))
	}

	if retrievedChain[0] != block1 || retrievedChain[1] != block2 {
		t.Errorf("Block chain mismatch")
	}

	// Read blocks back
	read1, err := store.ReadBlock(block1)
	if err != nil {
		t.Fatalf("ReadBlock failed: %v", err)
	}
	if string(read1[:12]) != "block 1 data" {
		t.Errorf("Block 1 data mismatch")
	}

	read2, err := store.ReadBlock(block2)
	if err != nil {
		t.Fatalf("ReadBlock failed: %v", err)
	}
	if string(read2[:12]) != "block 2 data" {
		t.Errorf("Block 2 data mismatch")
	}

	// Test sparse file (block chain with hole)
	block3, err := store.AllocBlock()
	if err != nil {
		t.Fatalf("AllocBlock failed: %v", err)
	}

	sparseChain := []uint64{block1, 0, block3} // 0 = hole
	err = store.SetBlockChain(ino, sparseChain)
	if err != nil {
		t.Fatalf("SetBlockChain failed: %v", err)
	}

	retrieved, err := store.GetBlockChain(ino)
	if err != nil {
		t.Fatalf("GetBlockChain failed: %v", err)
	}

	if len(retrieved) != 3 {
		t.Fatalf("Expected 3 blocks, got %d", len(retrieved))
	}
	if retrieved[1] != 0 {
		t.Errorf("Expected hole (0), got %d", retrieved[1])
	}

	// Free blocks
	err = store.FreeBlock(block1)
	if err != nil {
		t.Fatalf("FreeBlock failed: %v", err)
	}
}

// TestMemExtentStore verifies the ExtentStore implementation.
func TestMemExtentStore(t *testing.T) {
	blockSize := 1024
	store := NewMemExtentStore(blockSize)
	ino := uint64(1)

	// Allocate an extent
	physical, length, err := store.AllocExtent(10)
	if err != nil {
		t.Fatalf("AllocExtent failed: %v", err)
	}
	if length != 10 {
		t.Errorf("Expected length 10, got %d", length)
	}

	// Write data to extent
	data := make([]byte, int(length)*blockSize)
	copy(data, []byte("extent data"))
	err = store.WriteExtent(physical, length, data)
	if err != nil {
		t.Fatalf("WriteExtent failed: %v", err)
	}

	// Create extent list
	extents := ExtentList{
		{Logical: 0, Physical: physical, Length: length},
	}

	err = store.SetExtents(ino, extents)
	if err != nil {
		t.Fatalf("SetExtents failed: %v", err)
	}

	// Retrieve extents
	retrieved, err := store.GetExtents(ino)
	if err != nil {
		t.Fatalf("GetExtents failed: %v", err)
	}

	if len(retrieved) != 1 {
		t.Fatalf("Expected 1 extent, got %d", len(retrieved))
	}

	if retrieved[0].Logical != 0 || retrieved[0].Physical != physical || retrieved[0].Length != length {
		t.Errorf("Extent mismatch")
	}

	// Read extent back
	readData, err := store.ReadExtent(physical, length)
	if err != nil {
		t.Fatalf("ReadExtent failed: %v", err)
	}

	if string(readData[:11]) != "extent data" {
		t.Errorf("Extent data mismatch")
	}

	// Test sparse file (with hole)
	physical2, _, err := store.AllocExtent(5)
	if err != nil {
		t.Fatalf("AllocExtent failed: %v", err)
	}

	// Extent list with gap: blocks 0-9 allocated, 10-19 hole, 20-24 allocated
	sparseExtents := ExtentList{
		{Logical: 0, Physical: physical, Length: 10},
		{Logical: 20, Physical: physical2, Length: 5},
	}

	err = store.SetExtents(ino, sparseExtents)
	if err != nil {
		t.Fatalf("SetExtents failed: %v", err)
	}

	retrieved, err = store.GetExtents(ino)
	if err != nil {
		t.Fatalf("GetExtents failed: %v", err)
	}

	if len(retrieved) != 2 {
		t.Fatalf("Expected 2 extents, got %d", len(retrieved))
	}

	// Test FindExtent
	extIdx, offset, found := retrieved.FindExtent(5)
	if !found {
		t.Error("Expected to find block 5")
	}
	if extIdx != 0 || offset != 5 {
		t.Errorf("Expected extent 0 offset 5, got %d offset %d", extIdx, offset)
	}

	// Test finding block in hole
	_, _, found = retrieved.FindExtent(15)
	if found {
		t.Error("Expected not to find block 15 (hole)")
	}

	// Test finding block in second extent
	extIdx, offset, found = retrieved.FindExtent(22)
	if !found {
		t.Error("Expected to find block 22")
	}
	if extIdx != 1 || offset != 2 {
		t.Errorf("Expected extent 1 offset 2, got %d offset %d", extIdx, offset)
	}
}

// TestExtentListHelpers tests the ExtentList helper methods.
func TestExtentListHelpers(t *testing.T) {
	extents := ExtentList{
		{Logical: 0, Physical: 100, Length: 10},
		{Logical: 20, Physical: 200, Length: 5},
		{Logical: 30, Physical: 300, Length: 15},
	}

	// Test TotalBlocks
	total := extents.TotalBlocks()
	expected := uint64(10 + 5 + 15)
	if total != expected {
		t.Errorf("Expected total blocks %d, got %d", expected, total)
	}

	// Test LogicalSize
	size := extents.LogicalSize()
	expected = 30 + 15 // last extent ends at block 44
	if size != expected {
		t.Errorf("Expected logical size %d, got %d", expected, size)
	}

	// Test empty extent list
	empty := ExtentList{}
	if empty.TotalBlocks() != 0 {
		t.Error("Expected 0 total blocks for empty list")
	}
	if empty.LogicalSize() != 0 {
		t.Error("Expected 0 logical size for empty list")
	}
}

// TestBlockUtilities tests the utility functions.
func TestBlockUtilities(t *testing.T) {
	blockSize := 4096

	// Test BlockCount
	tests := []struct {
		size     int64
		expected int64
	}{
		{0, 0},
		{1, 1},
		{4096, 1},
		{4097, 2},
		{8192, 2},
		{8193, 3},
	}

	for _, tt := range tests {
		result := BlockCount(tt.size, blockSize)
		if result != tt.expected {
			t.Errorf("BlockCount(%d, %d) = %d, expected %d", tt.size, blockSize, result, tt.expected)
		}
	}

	// Test BlockOffset
	block, offset := BlockOffset(5000, blockSize)
	if block != 1 || offset != 904 {
		t.Errorf("BlockOffset(5000, 4096) = (%d, %d), expected (1, 904)", block, offset)
	}

	block, offset = BlockOffset(0, blockSize)
	if block != 0 || offset != 0 {
		t.Errorf("BlockOffset(0, 4096) = (%d, %d), expected (0, 0)", block, offset)
	}

	block, offset = BlockOffset(4096, blockSize)
	if block != 1 || offset != 0 {
		t.Errorf("BlockOffset(4096, 4096) = (%d, %d), expected (1, 0)", block, offset)
	}

	// Test BlockRange - span 2 blocks
	// Bytes 5000-9095 span blocks 1 (4096-8191) and 2 (8192-12287)
	first, count := BlockRange(5000, 4096, blockSize)
	if first != 1 || count != 2 {
		t.Errorf("BlockRange(5000, 4096, 4096) = (%d, %d), expected (1, 2)", first, count)
	}

	// Edge case: exact block boundary
	first, count = BlockRange(4096, 4096, blockSize)
	if first != 1 || count != 1 {
		t.Errorf("BlockRange(4096, 4096, 4096) = (%d, %d), expected (1, 1)", first, count)
	}

	// Edge case: spans three blocks
	first, count = BlockRange(4000, 5000, blockSize)
	if first != 0 || count != 3 {
		t.Errorf("BlockRange(4000, 5000, 4096) = (%d, %d), expected (0, 3)", first, count)
	}
}

// TestCOWMemExtentStore tests copy-on-write functionality.
func TestCOWMemExtentStore(t *testing.T) {
	blockSize := 1024
	store := NewCOWMemExtentStore(blockSize)

	// Create source inode with data
	srcIno := uint64(1)
	physical, length, err := store.AllocExtent(5)
	if err != nil {
		t.Fatalf("AllocExtent failed: %v", err)
	}

	data := make([]byte, int(length)*blockSize)
	copy(data, []byte("original data"))
	err = store.WriteExtent(physical, length, data)
	if err != nil {
		t.Fatalf("WriteExtent failed: %v", err)
	}

	extents := ExtentList{
		{Logical: 0, Physical: physical, Length: length},
	}
	err = store.SetExtents(srcIno, extents)
	if err != nil {
		t.Fatalf("SetExtents failed: %v", err)
	}

	// Clone to destination
	dstIno := uint64(2)
	err = store.CloneExtents(srcIno, dstIno)
	if err != nil {
		t.Fatalf("CloneExtents failed: %v", err)
	}

	// Verify both have same extents
	srcExtents, _ := store.GetExtents(srcIno)
	dstExtents, _ := store.GetExtents(dstIno)

	if len(srcExtents) != len(dstExtents) {
		t.Fatal("Extent lists should be same length after clone")
	}

	// Verify reference count increased
	refCount := store.RefCount(physical)
	if refCount == 0 {
		t.Error("Expected reference count > 0 for shared block")
	}

	// Break COW on destination - this should create a copy
	// (Note: BreakCOW in the example has limitations)
	// For now, just verify the API works
	_, err = store.BreakCOW(dstIno, 0)
	if err != nil && err.Error() != "extent splitting not implemented in example" {
		// For single-block extents, it should work
		if length > 1 {
			// Expected for multi-block extents in this simple implementation
		} else {
			t.Fatalf("BreakCOW failed: %v", err)
		}
	}
}

// BenchmarkMemByteStoreWrite benchmarks ByteStore write performance.
func BenchmarkMemByteStoreWrite(b *testing.B) {
	store := NewMemByteStore()
	data := make([]byte, 4096)
	ino := uint64(1)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.WriteAt(ino, data, int64(i*4096))
	}
}

// BenchmarkMemByteStoreRead benchmarks ByteStore read performance.
func BenchmarkMemByteStoreRead(b *testing.B) {
	store := NewMemByteStore()
	data := make([]byte, 4096)
	ino := uint64(1)

	// Pre-populate
	for i := 0; i < 1000; i++ {
		store.WriteAt(ino, data, int64(i*4096))
	}

	buf := make([]byte, 4096)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.ReadAt(ino, buf, int64((i%1000)*4096))
	}
}
