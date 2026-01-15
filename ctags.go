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

func (server *Server) ctagsArgs(extra ...string) []string {
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
		tagsPath := server.tagfilePath
		if !filepath.IsAbs(tagsPath) {
			tagsPath = filepath.Join(server.rootPath, tagsPath)
		}
		tagsPath = filepath.Clean(tagsPath)
		if _, err := os.Stat(tagsPath); err != nil {
			return fmt.Errorf("tagfile not found at %q: %v", tagsPath, err)
		}
		entries, err := parseTagfile(tagsPath, server.rootPath)
		if err != nil {
			return err
		}

		server.mutex.Lock()
		server.tagEntries = append(server.tagEntries, entries...)
		server.mutex.Unlock()
		return nil
	}

	if tagsPath, found := findTagsFile(server.rootPath); found {
		entries, err := parseTagfile(tagsPath, server.rootPath)
		if err != nil {
			return err
		}

		server.mutex.Lock()
		server.tagEntries = append(server.tagEntries, entries...)
		server.mutex.Unlock()
		return nil
	}

	files, err := listWorkspaceFiles(server.rootPath)
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

			cmd := exec.Command(server.ctagsBin, server.ctagsArgs("-L", "-")...)
			cmd.Dir = server.rootPath
			cmd.Stdin = strings.NewReader(strings.Join(chunk, "\n"))

			if err := server.processTagsOutput(cmd); err != nil {
				log.Printf("ctags error: %v", err)
			}
		}(chunk)
	}

	wg.Wait()
	return nil
}

// listWorkspaceFiles returns root-relative file paths using git, jj, or a directory walk.
func listWorkspaceFiles(root string) ([]string, error) {
	if isGitRepo(root) {
		output, err := exec.Command("git", "-C", root, "ls-files").Output()
		if err != nil {
			return nil, err
		}
		files := strings.Split(strings.TrimSpace(string(output)), "\n")
		return workspacePathsToRootRelative(root, files)
	}

	if isJjRepo(root) {
		output, err := exec.Command("jj", "file", "list", "--repository", root).Output()
		if err != nil {
			return nil, err
		}
		files := strings.Split(strings.TrimSpace(string(output)), "\n")
		return workspacePathsToRootRelative(root, files)
	}

	var files []string
	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if rel, e := filepath.Rel(root, path); e == nil {
				files = append(files, rel)
			}
		}
		return nil
	})
	return workspacePathsToRootRelative(root, files)
}

func isGitRepo(path string) bool {
	cmd := exec.Command("git", "-C", path, "rev-parse", "--is-inside-work-tree")
	return cmd.Run() == nil
}

func isJjRepo(path string) bool {
	cmd := exec.Command("jj", "repo", "info", "--repository", path)
	return cmd.Run() == nil
}

// workspacePathsToRootRelative normalizes candidate workspace paths to root-relative form.
func workspacePathsToRootRelative(rootPath string, paths []string) ([]string, error) {
	relativePaths := make([]string, 0, len(paths))
	for _, path := range paths {
		rel, err := toRootRelativePath(rootPath, path)
		if err != nil {
			return nil, err
		}
		relativePaths = append(relativePaths, rel)
	}
	return relativePaths, nil
}

// scanSingleFileTag rescans a single file and drops any previous entries for that path.
func (server *Server) scanSingleFileTag(filePath string) error {
	if strings.HasPrefix(filePath, "..") {
		return fmt.Errorf("path outside root: %s", filePath)
	}

	server.mutex.Lock()
	newEntries := make([]TagEntry, 0, len(server.tagEntries))
	for _, entry := range server.tagEntries {
		if entry.Path != filePath {
			newEntries = append(newEntries, entry)
		}
	}
	server.tagEntries = newEntries
	server.mutex.Unlock()

	cmd := exec.Command(server.ctagsBin, server.ctagsArgs(filePath)...)
	cmd.Dir = server.rootPath
	return server.processTagsOutput(cmd)
}

func (server *Server) processTagsOutput(cmd *exec.Cmd) error {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdout from ctags command: %v", err)
	}

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

		relPath, err := toRootRelativePath(server.rootPath, entry.Path)
		if err != nil {
			log.Printf("Failed to make path relative for %s: %v", entry.Path, err)
			continue
		}
		entry.Path = relPath

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
