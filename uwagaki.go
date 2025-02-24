// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2025 Hajime Hoshi

package uwagaki

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/mod/modfile"
)

// ReplaceItem represents a file replacement.
type ReplaceItem struct {
	// Mod is a module path.
	Mod string

	// Path is a file path in the module.
	Path string

	// Content is a file content.
	Content []byte
}

// CreateEnvironment creates a directory and returns it where you can run go commands.
// The returned directory includes go.mod and go.sum files to replace the specified files.
//
// pkgs is a list of package paths. pkgs is passed to 'go get' command to create go.mod.
//
// The returned directory is temporary and you should remove it after using it.
//
// If the current directory or its parent directories has go.mod, CreateEnvironment uses it
// as the base go.mod. Otherwise, CreateEnvironment creates a new go.mod by 'go mod init'.
//
// Usually, Go's -overlay flag cannot be used for external modules (see https://go.dev/cl/650475).
// CreateEnvironment creates a temporary environment to replace files in external modules by go.mod.
func CreateEnvironment(pkgs []string, replaces []ReplaceItem) (work string, err error) {
	work, err = os.MkdirTemp("", "")
	if err != nil {
		return "", err
	}

	// If the current directory has go.mod, use this.
	var currentGoMod string
	{
		cmd := exec.Command("go", "list", "-m", "-f", "{{.GoMod}}")
		out, err := cmd.Output()
		if err == nil {
			// Ignore the error.
			currentGoMod = strings.TrimSpace(string(out))
		}
	}

	randomModuleName := "uwagaki_" + time.Now().UTC().Format("20060102150405")

	var origModPath string
	if currentGoMod != "" {
		// Copy the current go.mod and go.sum to the work directory, but with modifying the module name.
		content, err := os.ReadFile(currentGoMod)
		if err != nil {
			return "", err
		}
		mod, err := modfile.ParseLax(currentGoMod, content, nil)
		if err != nil {
			return "", err
		}
		origModPath = mod.Module.Mod.Path
		// TODO: Copy mod.Replace.
		if err := mod.AddModuleStmt(randomModuleName); err != nil {
			return "", err
		}
		content2, err := mod.Format()
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(work, "go.mod"), content2, 0644); err != nil {
			return "", err
		}
	} else {
		// go mod init
		var buf bytes.Buffer
		cmd := exec.Command("go", "mod", "init", randomModuleName)
		cmd.Stderr = &buf
		cmd.Dir = work
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("uwagaki: '%s' failed: %w\n%s", strings.Join(cmd.Args, " "), err, buf.String())
		}
	}

	// go get
	{
		var resolvedPath []string
		for _, pkg := range pkgs {
			// go-get'ing with reolative paths doesn't make sense. Skip them.
			if modfile.IsDirectoryPath(pkg) {
				continue
			}
			resolvedPath = append(resolvedPath, pkg)
		}
		if len(resolvedPath) > 0 {
			// go get
			var buf bytes.Buffer
			cmd := exec.Command("go", "get")
			cmd.Args = append(cmd.Args, resolvedPath...)
			cmd.Stderr = &buf
			cmd.Dir = work
			if err := cmd.Run(); err != nil {
				return "", fmt.Errorf("uwagaki: '%s' failed: %w\n%s", strings.Join(cmd.Args, " "), err, buf.String())
			}
		}
	}

	// Redirect the current module to its current source, espcially for directory packge paths.
	if origModPath != "" {
		// go get (to update go.sum)
		{
			var buf bytes.Buffer
			cmd := exec.Command("go", "get", origModPath)
			cmd.Stderr = &buf
			cmd.Dir = work
			if err := cmd.Run(); err != nil {
				return "", fmt.Errorf("uwagaki: '%s' failed: %w\n%s", strings.Join(cmd.Args, " "), err, buf.String())
			}
		}
		// go mod edit
		{
			dstRel := filepath.Dir(currentGoMod)
			var buf bytes.Buffer
			// TODO: What if the file path includes a space?
			cmd := exec.Command("go", "mod", "edit", "-replace", origModPath+"="+dstRel)
			cmd.Stderr = &buf
			cmd.Dir = work
			if err := cmd.Run(); err != nil {
				return "", fmt.Errorf("uwagaki: '%s' failed: %w\n%s", strings.Join(cmd.Args, " "), err, buf.String())
			}
		}
	}

	replacedModDir := filepath.Join(work, "mod")

	modVisited := map[string]struct{}{}
	for _, r := range replaces {
		if _, ok := modVisited[r.Mod]; !ok {
			// go get
			{
				var buf bytes.Buffer
				cmd := exec.Command("go", "get", r.Mod)
				cmd.Stderr = &buf
				cmd.Dir = work
				if err := cmd.Run(); err != nil {
					return "", fmt.Errorf("uwagaki: '%s' failed: %w\n%s", strings.Join(cmd.Args, " "), err, buf.String())
				}
			}
			// go list
			var modFilepath string
			{
				var buf bytes.Buffer
				cmd := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", r.Mod)
				cmd.Stderr = &buf
				cmd.Dir = work
				out, err := cmd.Output()
				if err != nil {
					return "", fmt.Errorf("uwagaki: '%s' failed: %w\n%s", strings.Join(cmd.Args, " "), err, buf.String())
				}
				modFilepath = strings.TrimSpace(string(out))
			}

			if err := replace(work, replacedModDir, r.Mod, modFilepath); err != nil {
				return "", err
			}

			modVisited[r.Mod] = struct{}{}
		}

		dst := filepath.Join(replacedModDir, filepath.FromSlash(r.Mod), filepath.FromSlash(r.Path))
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			return "", err
		}
		if err := os.WriteFile(dst, r.Content, 0644); err != nil {
			return "", err
		}
	}

	return work, nil
}

func replace(work string, replacedFilesDir string, modulePath string, moduleSrcFilepath string) error {
	// Copy files.
	dst := filepath.Join(replacedFilesDir, filepath.FromSlash(modulePath))
	f, err := os.Stat(dst)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil && !f.IsDir() {
		return fmt.Errorf("uwagaki: %s is not a directory", dst)
	}
	if errors.Is(err, os.ErrNotExist) {
		// TODO: Use symbolic linkes instead of copying files.
		if err := os.CopyFS(dst, os.DirFS(moduleSrcFilepath)); err != nil {
			return err
		}
	}

	// go mod edit
	{
		dstRel := "." + string(filepath.Separator) + filepath.Join("mod", filepath.FromSlash(modulePath))
		var buf bytes.Buffer
		// TODO: What if the file path includes a space?
		cmd := exec.Command("go", "mod", "edit", "-replace", modulePath+"="+dstRel)
		cmd.Stderr = &buf
		cmd.Dir = work
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("uwagaki: '%s' failed: %w\n%s", strings.Join(cmd.Args, " "), err, buf.String())
		}
	}

	return nil
}

// ResolvePaths resolves package paths for the specified environment directory.
// A relative package path will be resolved to a path with a module path if go.mod exists,
// or an absolute path otherwise.
//
// The returned value can be passed to go commands like 'go run' at the working
// directory envDir.
//
// If pkgs is empty, ResolvePaths returns a module path if go.mod exists,
// or an absolute path for the current directory otherwise.
func ResolvePaths(envDir string, pkgs []string) ([]string, error) {
	if len(pkgs) == 0 {
		pkgs = []string{"."}
	}

	newPkgs := make([]string, len(pkgs))
	for i, pkg := range pkgs {
		if !modfile.IsDirectoryPath(pkg) {
			newPkgs[i] = pkg
			continue
		}

		var currentGoMod string
		{
			cmd := exec.Command("go", "list", "-m", "-f", "{{.GoMod}}")
			cmd.Dir = filepath.Dir(pkg)
			out, err := cmd.Output()
			if err == nil {
				// Ignore the error.
				currentGoMod = strings.TrimSpace(string(out))
			}
		}

		abs, err := filepath.Abs(pkg)
		if err != nil {
			return nil, err
		}

		if currentGoMod == "" {
			newPkgs[i] = abs
			continue
		}

		var currentModPath string
		{
			cmd := exec.Command("go", "list", "-m", "-f", "{{.Path}}")
			cmd.Dir = filepath.Dir(pkg)
			out, err := cmd.Output()
			if err != nil {
				if ee, ok := err.(*exec.ExitError); ok {
					return nil, fmt.Errorf("uwagaki: '%s' failed: %w\n%s", strings.Join(cmd.Args, " "), ee, ee.Stderr)
				}
				return nil, err
			}
			currentModPath = strings.TrimSpace(string(out))
		}

		rel, err := filepath.Rel(filepath.Dir(currentGoMod), abs)
		if err != nil {
			return nil, err
		}
		newPkgs[i] = path.Join(currentModPath, filepath.ToSlash(rel))
	}
	return newPkgs, nil
}
