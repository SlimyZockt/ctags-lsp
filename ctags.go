package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

func (server *Server) parseCtagsArgs(extra ...string) []string {
	args := []string{"--output-format=json", "--fields=+n"}
	if server.languages != "" {
		args = append(args, "--languages="+server.languages)
	}
	return append(args, extra...)
}

// scanWorkspace populates `server.tagEntries` from either:
// - an explicit `--tagfile`, then
// - a discovered tags file (see `findTagsFile`), or
// - a fresh ctags scan of the workspace.
func (server *Server) scanWorkspace() error {
	if server.tagfilePath != "" {
		rootDir := fileURIToPath(server.rootURI)
		tagsPath := server.tagfilePath
		if !filepath.IsAbs(tagsPath) {
			tagsPath = filepath.Join(rootDir, tagsPath)
		}
		tagsPath = filepath.Clean(tagsPath)
		if _, err := os.Stat(tagsPath); err != nil {
			return fmt.Errorf("tagfile not found at %q: %v", tagsPath, err)
		}
		entries, err := parseTagfile(tagsPath)
		if err != nil {
			return err
		}

		server.mutex.Lock()
		server.tagEntries = append(server.tagEntries, entries...)
		server.mutex.Unlock()
		return nil
	}

	rootDir := fileURIToPath(server.rootURI)
	if tagsPath, found := findTagsFile(rootDir); found {
		entries, err := parseTagfile(tagsPath)
		if err != nil {
			return err
		}

		server.mutex.Lock()
		server.tagEntries = append(server.tagEntries, entries...)
		server.mutex.Unlock()
		return nil
	}

	files, err := listWorkspaceFiles(rootDir)
	if err != nil {
		return err
	}

	workers := runtime.NumCPU()
	size := (len(files) + workers - 1) / workers
	var wg sync.WaitGroup

	for i := range workers {
		start := i * size
		if start >= len(files) {
			break
		}
		end := min(start+size, len(files))
		chunk := files[start:end]

		wg.Add(1)
		go func(chunk []string) {
			defer wg.Done()

			cmd := exec.Command(server.ctagsBin, server.parseCtagsArgs("-L", "-")...)
			cmd.Dir = rootDir
			cmd.Stdin = strings.NewReader(strings.Join(chunk, "\n"))

			if err := server.processTagsOutput(cmd); err != nil {
				log.Printf("ctags error: %v", err)
			}
		}(chunk)
	}

	wg.Wait()
	return nil
}

// listWorkspaceFiles returns file paths using git, jj, or a directory walk.
// These paths are not normalized and may be relative or absolute.
func listWorkspaceFiles(rootDir string) ([]string, error) {
	if isGitRepo(rootDir) {
		output, err := exec.Command("git", "-C", rootDir, "ls-files").Output()
		if err != nil {
			return nil, err
		}
		files := strings.Split(strings.TrimSpace(string(output)), "\n")
		return files, nil
	}

	if isJjRepo(rootDir) {
		output, err := exec.Command("jj", "file", "list", "--repository", rootDir).Output()
		if err != nil {
			return nil, err
		}
		files := strings.Split(strings.TrimSpace(string(output)), "\n")
		return files, nil
	}

	var files []string
	filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	return files, nil
}

func isGitRepo(path string) bool {
	cmd := exec.Command("git", "-C", path, "rev-parse", "--is-inside-work-tree")
	return cmd.Run() == nil
}

func isJjRepo(path string) bool {
	cmd := exec.Command("jj", "repo", "info", "--repository", path)
	return cmd.Run() == nil
}

// scanSingleFileTag rescans a single file URI and drops any previous entries for that URI.
func (server *Server) scanSingleFileTag(fileURI string) error {
	server.mutex.Lock()
	newEntries := make([]TagEntry, 0, len(server.tagEntries))
	for _, entry := range server.tagEntries {
		if entry.Path != fileURI {
			newEntries = append(newEntries, entry)
		}
	}
	server.tagEntries = newEntries
	server.mutex.Unlock()

	filePath := fileURIToPath(fileURI)
	tmp := []string{filePath}
	cmd := exec.Command(server.ctagsBin, server.parseCtagsArgs(append(tmp, server.ctagArgs...)...)...)
	rootDir := fileURIToPath(server.rootURI)
	cmd.Dir = rootDir
	return server.processTagsOutput(cmd)
}

func (server *Server) processTagsOutput(cmd *exec.Cmd) error {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdout from ctags command: %v", err)
	}

	rootDir := fileURIToPath(server.rootURI)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start ctags command: %v", err)
	}

	scanner := bufio.NewScanner(stdout)
	var entries []TagEntry
	for scanner.Scan() {
		var entry TagEntry
		if err := json.Unmarshal([]byte(scanner.Text()), &entry); err != nil {
			log.Printf("Failed to parse ctags JSON entry: %v", err)
			continue
		}

		normalized, err := normalizePath(rootDir, entry.Path)
		if err != nil {
			log.Printf("Failed to normalize path for %s: %v", entry.Path, err)
			continue
		}
		entry.Path = pathToFileURI(normalized)

		entries = append(entries, entry)
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading ctags output: %v", err)
	}

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("ctags command failed: %v", err)
	}

	server.mutex.Lock()
	server.tagEntries = append(server.tagEntries, entries...)
	server.mutex.Unlock()

	return nil
}
