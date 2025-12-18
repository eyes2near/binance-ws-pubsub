
package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// The name of the executable to be ignored.
const executableName = "project-struct.exe"

// List of directories and files to ignore.
var ignoreList = map[string]bool{
	".git":           true,
	"sum.md":         true,
	executableName:   true,
	"tools":          true, // Ignore the tool's own source directory name
	"tmp":            true, // Ignore the tmp directory in cmd
}

func main() {
	// Get the current working directory.
	rootDir, err := os.Getwd()
	if err != nil {
		fmt.Printf("Error getting current directory: %v\n", err)
		os.Exit(1)
	}

	// Get the base name of the root directory.
	rootBase := filepath.Base(rootDir)

	var builder strings.Builder
	builder.WriteString("项目结构图如下:\n")
	builder.WriteString(rootBase + "/\n")

	// Generate the tree structure starting from the root ".".
	err = generateTree(&builder, ".", "")
	if err != nil {
		fmt.Printf("Error generating tree: %v\n", err)
		os.Exit(1)
	}

	// Write the final string to sum.md file.
	err = os.WriteFile("sum.md", []byte(builder.String()), 0644)
	if err != nil {
		fmt.Printf("Error writing to sum.md: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Successfully generated %s\n", "sum.md")
}

// generateTree recursively walks the directory and builds a string representation.
func generateTree(builder *strings.Builder, dirPath, prefix string) error {
	// Read all entries in the directory.
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return err
	}

	// Filter out ignored files and directories.
	filteredEntries := make([]fs.DirEntry, 0)
	for _, entry := range entries {
		// Check against the ignore list by name.
		if _, ignored := ignoreList[entry.Name()]; ignored {
			continue
		}
		// Ignore any .exe files.
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".exe" {
			continue
		}
		filteredEntries = append(filteredEntries, entry)
	}

	// Sort entries to have a consistent order (dirs first, then files).
	sort.Slice(filteredEntries, func(i, j int) bool {
		if filteredEntries[i].IsDir() != filteredEntries[j].IsDir() {
			return filteredEntries[i].IsDir()
		}
		return filteredEntries[i].Name() < filteredEntries[j].Name()
	})

	for i, entry := range filteredEntries {
		connector := "├───"
		pathPrefix := "│   "
		if i == len(filteredEntries)-1 {
			connector = "└───"
			pathPrefix = "    "
		}

		builder.WriteString(prefix)
		builder.WriteString(connector)
		builder.WriteString(entry.Name())

		if entry.IsDir() {
			builder.WriteString("/")
		}
		builder.WriteString("\n")

		if entry.IsDir() {
			nextPath := filepath.Join(dirPath, entry.Name())
			// Recursive call for directories.
			generateTree(builder, nextPath, prefix+pathPrefix)
		}
	}

	return nil
}
