package inode

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

// TestConcurrentInoAllocation verifies that Ino allocation is thread-safe
// and produces unique inode numbers under concurrent access.
func TestConcurrentInoAllocation(t *testing.T) {
	var ino Ino
	const goroutines = 100
	const allocsPerGoroutine = 1000

	var wg sync.WaitGroup
	inodes := make(chan *Inode, goroutines*allocsPerGoroutine)

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < allocsPerGoroutine; j++ {
				node := ino.New(0644)
				inodes <- node
			}
		}()
	}

	wg.Wait()
	close(inodes)

	// Verify all inode numbers are unique
	seen := make(map[uint64]bool)
	for node := range inodes {
		if seen[node.Ino] {
			t.Errorf("duplicate inode number: %d", node.Ino)
		}
		seen[node.Ino] = true
	}

	expected := goroutines * allocsPerGoroutine
	if len(seen) != expected {
		t.Errorf("expected %d unique inodes, got %d", expected, len(seen))
	}
}

// TestConcurrentDirAllocation verifies that NewDir is thread-safe.
func TestConcurrentDirAllocation(t *testing.T) {
	var ino Ino
	const goroutines = 50
	const allocsPerGoroutine = 100

	var wg sync.WaitGroup
	dirs := make(chan *Inode, goroutines*allocsPerGoroutine)

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < allocsPerGoroutine; j++ {
				dir := ino.NewDir(0755)
				dirs <- dir
			}
		}()
	}

	wg.Wait()
	close(dirs)

	// Verify all directories have proper structure
	seen := make(map[uint64]bool)
	for dir := range dirs {
		if seen[dir.Ino] {
			t.Errorf("duplicate inode number: %d", dir.Ino)
		}
		seen[dir.Ino] = true

		// Verify . and .. entries exist
		if len(dir.Dir) < 2 {
			t.Errorf("dir %d missing . and .. entries", dir.Ino)
		}
	}
}

// TestConcurrentLinkUnlink verifies that Link and Unlink operations
// are thread-safe when operating on the same directory.
func TestConcurrentLinkUnlink(t *testing.T) {
	var ino Ino
	root := ino.NewDir(0755)

	const goroutines = 50
	const opsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				name := fmt.Sprintf("file_%d_%d", id, j)
				node := ino.New(0644)

				// Link
				err := root.Link(name, node)
				if err != nil {
					t.Errorf("Link failed: %v", err)
					return
				}

				// Unlink
				err = root.Unlink(name)
				if err != nil {
					t.Errorf("Unlink failed: %v", err)
					return
				}
			}
		}(i)
	}

	wg.Wait()

	// Directory should only have . and .. entries
	entries := root.ReadDir()
	actualCount := 0
	for _, e := range entries {
		if e.Name() != "." && e.Name() != ".." {
			actualCount++
		}
	}
	if actualCount != 0 {
		t.Errorf("expected 0 entries (excluding . and ..), got %d", actualCount)
	}
}

// TestConcurrentResolve verifies that Resolve operations are thread-safe
// and can run concurrently with Link/Unlink operations.
func TestConcurrentResolve(t *testing.T) {
	var ino Ino
	root := ino.NewDir(0755)

	// Create a directory structure
	dir1 := ino.NewDir(0755)
	root.Link("dir1", dir1)
	dir1.Link("..", root)

	dir2 := ino.NewDir(0755)
	dir1.Link("dir2", dir2)
	dir2.Link("..", dir1)

	file := ino.New(0644)
	dir2.Link("file.txt", file)

	const goroutines = 100
	const opsPerGoroutine = 100

	var wg sync.WaitGroup
	var resolveCount atomic.Int64
	var errorCount atomic.Int64

	// Spawn goroutines that continuously resolve paths
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			paths := []string{
				"/",
				"/dir1",
				"/dir1/dir2",
				"/dir1/dir2/file.txt",
				"/dir1/..",
				"/dir1/dir2/..",
			}
			for j := 0; j < opsPerGoroutine; j++ {
				path := paths[j%len(paths)]
				_, err := root.Resolve(path)
				if err != nil {
					errorCount.Add(1)
				} else {
					resolveCount.Add(1)
				}
			}
		}()
	}

	wg.Wait()

	if errorCount.Load() > 0 {
		t.Errorf("resolve errors: %d", errorCount.Load())
	}
	t.Logf("successful resolves: %d", resolveCount.Load())
}

// TestConcurrentLinkResolve verifies that Link and Resolve can run concurrently.
func TestConcurrentLinkResolve(t *testing.T) {
	var ino Ino
	root := ino.NewDir(0755)

	const writers = 10
	const readers = 50
	const opsPerGoroutine = 100

	var wg sync.WaitGroup

	// Writer goroutines that add files
	wg.Add(writers)
	for i := 0; i < writers; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				name := fmt.Sprintf("file_%d_%d", id, j)
				node := ino.New(0644)
				root.Link(name, node)
			}
		}(i)
	}

	// Reader goroutines that resolve paths
	wg.Add(readers)
	for i := 0; i < readers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				// Resolve root - should always succeed
				_, err := root.Resolve("/")
				if err != nil {
					t.Errorf("failed to resolve root: %v", err)
					return
				}

				// Resolve . - should always succeed
				_, err = root.Resolve(".")
				if err != nil {
					t.Errorf("failed to resolve .: %v", err)
					return
				}
			}
		}()
	}

	wg.Wait()

	// Verify final state
	entries := root.ReadDir()
	expectedFiles := writers * opsPerGoroutine
	actualFiles := 0
	for _, e := range entries {
		if e.Name() != "." && e.Name() != ".." {
			actualFiles++
		}
	}
	if actualFiles != expectedFiles {
		t.Errorf("expected %d files, got %d", expectedFiles, actualFiles)
	}
}

// TestConcurrentRename verifies that Rename operations are thread-safe.
func TestConcurrentRename(t *testing.T) {
	var ino Ino
	root := ino.NewDir(0755)

	// Create two directories
	dir1 := ino.NewDir(0755)
	root.Link("dir1", dir1)
	dir1.Link("..", root)

	dir2 := ino.NewDir(0755)
	root.Link("dir2", dir2)
	dir2.Link("..", root)

	// Create files in dir1
	const numFiles = 100
	for i := 0; i < numFiles; i++ {
		file := ino.New(0644)
		dir1.Link(fmt.Sprintf("file_%03d", i), file)
	}

	const goroutines = 10
	var wg sync.WaitGroup
	var successCount atomic.Int64

	// Move files from dir1 to dir2 concurrently
	wg.Add(goroutines)
	filesPerGoroutine := numFiles / goroutines
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			start := id * filesPerGoroutine
			end := start + filesPerGoroutine
			for j := start; j < end; j++ {
				oldpath := fmt.Sprintf("/dir1/file_%03d", j)
				newpath := fmt.Sprintf("/dir2/file_%03d", j)
				err := root.Rename(oldpath, newpath)
				if err == nil {
					successCount.Add(1)
				}
			}
		}(i)
	}

	wg.Wait()

	// Verify all files were moved
	if successCount.Load() != numFiles {
		t.Errorf("expected %d successful renames, got %d", numFiles, successCount.Load())
	}

	// Verify dir1 is empty (except . and ..)
	dir1Entries := dir1.ReadDir()
	dir1Files := 0
	for _, e := range dir1Entries {
		if e.Name() != "." && e.Name() != ".." {
			dir1Files++
		}
	}
	if dir1Files != 0 {
		t.Errorf("dir1 should be empty, has %d files", dir1Files)
	}

	// Verify dir2 has all files
	dir2Entries := dir2.ReadDir()
	dir2Files := 0
	for _, e := range dir2Entries {
		if e.Name() != "." && e.Name() != ".." {
			dir2Files++
		}
	}
	if dir2Files != numFiles {
		t.Errorf("dir2 should have %d files, has %d", numFiles, dir2Files)
	}
}

// TestConcurrentNlinkUpdates verifies that Nlink updates are thread-safe.
func TestConcurrentNlinkUpdates(t *testing.T) {
	var ino Ino
	node := ino.New(0644)

	const goroutines = 100
	const opsPerGoroutine = 1000

	var wg sync.WaitGroup

	// Spawn goroutines that increment the link count
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				node.countUp()
			}
		}()
	}

	wg.Wait()

	expected := uint64(goroutines * opsPerGoroutine)
	if node.Nlink != expected {
		t.Errorf("expected Nlink=%d, got %d", expected, node.Nlink)
	}

	// Now decrement back to 0
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				node.countDown()
			}
		}()
	}

	wg.Wait()

	if node.Nlink != 0 {
		t.Errorf("expected Nlink=0, got %d", node.Nlink)
	}
}

// TestConcurrentReadDir verifies that ReadDir returns consistent snapshots.
func TestConcurrentReadDir(t *testing.T) {
	var ino Ino
	root := ino.NewDir(0755)

	// Pre-populate with some files
	const initialFiles = 50
	for i := 0; i < initialFiles; i++ {
		node := ino.New(0644)
		root.Link(fmt.Sprintf("file_%03d", i), node)
	}

	const writers = 5
	const readers = 20
	const ops = 100

	var wg sync.WaitGroup

	// Writers add more files
	wg.Add(writers)
	for i := 0; i < writers; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < ops; j++ {
				node := ino.New(0644)
				root.Link(fmt.Sprintf("new_%d_%d", id, j), node)
			}
		}(i)
	}

	// Readers get directory snapshots
	wg.Add(readers)
	for i := 0; i < readers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < ops; j++ {
				entries := root.ReadDir()
				// Verify snapshot is consistent (no duplicate names)
				seen := make(map[string]bool, len(entries))
				for _, e := range entries {
					if seen[e.Name()] {
						t.Errorf("duplicate entry: %s", e.Name())
						return
					}
					seen[e.Name()] = true
				}
			}
		}()
	}

	wg.Wait()
}

// TestConcurrentLookup verifies that Lookup is thread-safe.
func TestConcurrentLookup(t *testing.T) {
	var ino Ino
	root := ino.NewDir(0755)

	// Create some files
	const numFiles = 100
	for i := 0; i < numFiles; i++ {
		node := ino.New(0644)
		root.Link(fmt.Sprintf("file_%03d", i), node)
	}

	const goroutines = 50
	const opsPerGoroutine = 200

	var wg sync.WaitGroup
	var found atomic.Int64
	var notFound atomic.Int64

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				// Look up existing file
				name := fmt.Sprintf("file_%03d", j%numFiles)
				entry := root.Lookup(name)
				if entry != nil {
					found.Add(1)
				} else {
					notFound.Add(1)
				}

				// Look up non-existing file
				entry = root.Lookup("nonexistent")
				if entry == nil {
					found.Add(1) // correct behavior
				}
			}
		}()
	}

	wg.Wait()

	if notFound.Load() > 0 {
		t.Errorf("failed to find %d entries that should exist", notFound.Load())
	}
}

// BenchmarkConcurrentResolve measures concurrent Resolve performance.
func BenchmarkConcurrentResolve(b *testing.B) {
	var ino Ino
	root := ino.NewDir(0755)

	// Create a deep directory structure
	current := root
	for i := 0; i < 10; i++ {
		dir := ino.NewDir(0755)
		current.Link(fmt.Sprintf("dir%d", i), dir)
		dir.Link("..", current)
		current = dir
	}

	// Add a file at the bottom
	file := ino.New(0644)
	current.Link("file.txt", file)

	path := "/dir0/dir1/dir2/dir3/dir4/dir5/dir6/dir7/dir8/dir9/file.txt"

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := root.Resolve(path)
			if err != nil {
				b.Errorf("Resolve failed: %v", err)
			}
		}
	})
}

// BenchmarkConcurrentLink measures concurrent Link performance.
func BenchmarkConcurrentLink(b *testing.B) {
	var ino Ino
	root := ino.NewDir(0755)
	var counter atomic.Int64

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			id := counter.Add(1)
			node := ino.New(0644)
			name := fmt.Sprintf("file_%d", id)
			err := root.Link(name, node)
			if err != nil {
				b.Errorf("Link failed: %v", err)
			}
		}
	})
}

// BenchmarkInoAllocation measures Ino allocation performance.
func BenchmarkInoAllocation(b *testing.B) {
	var ino Ino

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = ino.New(0644)
		}
	})
}
