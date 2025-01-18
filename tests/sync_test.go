package main

import (
	"gorsync/internal/client"
	"gorsync/internal/server"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func createMockFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()

	for name, content := range files {
		filePath := filepath.Join(dir, name)
		err := os.MkdirAll(filepath.Dir(filePath), 0755)
		assert.NoError(t, err)

		err = os.WriteFile(filePath, []byte(content), 0644)
		assert.NoError(t, err)
	}
}

func readAllFiles(t *testing.T, dir string) map[string]string {
	t.Helper()

	files := make(map[string]string)
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		assert.NoError(t, err)

		if !info.IsDir() {
			relPath, err := filepath.Rel(dir, path)
			assert.NoError(t, err)

			content, err := os.ReadFile(path)
			assert.NoError(t, err)

			files[relPath] = string(content)
		}
		return nil
	})
	assert.NoError(t, err)
	return files
}

func TestDirectorySync(t *testing.T) {
	// Create temporary directories for source and destination
	sourceDir, err := os.MkdirTemp(".", "source")
	assert.NoError(t, err)
	defer os.RemoveAll(sourceDir)

	destDir, err := os.MkdirTemp(".", "dest")
	assert.NoError(t, err)
	defer os.RemoveAll(destDir)

	// Mock files to sync
	mockFiles := map[string]string{
		"file1.txt":            "Content of file1",
		"subdir/file2":         "Content of file2",
		"subdir/file3":         "Content of file3",
		"file4.txt":            "Content of file4",
		"nested/dir/file5.txt": "Content of file5",
	}
	createMockFiles(t, sourceDir, mockFiles)

	// Start the server with a WaitGroup for synchronization
	var wg sync.WaitGroup
	wg.Add(1)

	destAddr := "127.0.0.1:9090"
	go func() {
		defer wg.Done()
		err := server.Start(destAddr, destDir)
		assert.NoError(t, err)
	}()

	// Wait for the server to start
	time.Sleep(500 * time.Millisecond) // Adjust timing if necessary

	// Run the client to sync the directory
	err = client.Start(sourceDir, destAddr, true)
	assert.NoError(t, err)

	// Wait for the server to finish processing
	wg.Wait()

	// Validate that all files are in the destination directory
	expectedFiles := mockFiles
	actualFiles := readAllFiles(t, destDir)
	assert.Equal(t, expectedFiles, actualFiles)
}
