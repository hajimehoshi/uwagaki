// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2025 Hajime Hoshi

package uwagaki

import (
	"bytes"
	"errors"
	"fmt"
	"io"
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

// CreateEnvironment returns a new directory where you can run go commands,
// and resolved paths that can be used in the new environment.
// The returned directory includes go.mod and go.sum files to replace the specified files.
// The returned paths can be passed to Go commands like 'go run' in the new environment.
//
// paths is a list of package paths that is passed to 'go get' command to create go.mod.
//
// The returned directory is temporary and you should remove it after using it.
//
// If the current directory or its parent directories has go.mod, CreateEnvironment uses it
// as the base go.mod. Otherwise, CreateEnvironment creates a new go.mod by 'go mod init'.
//
// Usually, Go's -overlay flag cannot be used for external modules (see https://go.dev/cl/650475).
// CreateEnvironment creates a temporary environment to replace files in external modules by go.mod.
func CreateEnvironment(paths []string, replaces []ReplaceItem) (workDir string, newPaths []string, err error) {
	work, err := os.MkdirTemp("", "")
	if err != nil {
		return "", nil, err
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
			return "", nil, err
		}
		mod, err := modfile.Parse(currentGoMod, content, nil)
		if err != nil {
			return "", nil, err
		}
		origModPath = mod.Module.Mod.Path
		if err := mod.AddModuleStmt(randomModuleName); err != nil {
			return "", nil, err
		}

		// Fix the 'replace' paths.
		dir := filepath.Dir(currentGoMod)
		// Copy the slice as AddReplace might affect the original slice.
		replaces := make([]*modfile.Replace, len(mod.Replace))
		copy(replaces, mod.Replace)
		for _, r := range replaces {
			if !modfile.IsDirectoryPath(r.New.Path) {
				continue
			}
			if filepath.IsAbs(r.New.Path) {
				continue
			}
			mod.AddReplace(r.Old.Path, r.Old.Version, filepath.Join(dir, r.New.Path), r.New.Version)
		}

		// Write the new go.mod.
		content2, err := mod.Format()
		if err != nil {
			return "", nil, err
		}
		if err := os.WriteFile(filepath.Join(work, "go.mod"), content2, 0644); err != nil {
			return "", nil, err
		}

		// Copy go.sum if exists.
		goSum := strings.TrimSuffix(currentGoMod, ".mod") + ".sum"
		if _, err := os.Stat(goSum); err == nil {
			content, err := os.ReadFile(goSum)
			if err != nil {
				return "", nil, err
			}
			if err := os.WriteFile(filepath.Join(work, "go.sum"), content, 0644); err != nil {
				return "", nil, err
			}
		}
	} else {
		// go mod init
		var buf bytes.Buffer
		cmd := exec.Command("go", "mod", "init", randomModuleName)
		cmd.Stderr = &buf
		cmd.Dir = work
		if err := cmd.Run(); err != nil {
			return "", nil, fmt.Errorf("uwagaki: '%s' failed: %w\n%s", strings.Join(cmd.Args, " "), err, buf.String())
		}
	}

	// go get
	{
		var nonDirPaths []string
		for _, path := range paths {
			// go-get'ing with relative paths doesn't make sense. Skip them.
			if modfile.IsDirectoryPath(path) {
				continue
			}
			nonDirPaths = append(nonDirPaths, path)
		}
		if len(nonDirPaths) > 0 {
			// go get
			var buf bytes.Buffer
			cmd := exec.Command("go", "get")
			cmd.Args = append(cmd.Args, nonDirPaths...)
			cmd.Stderr = &buf
			cmd.Dir = work
			if err := cmd.Run(); err != nil {
				return "", nil, fmt.Errorf("uwagaki: '%s' failed: %w\n%s", strings.Join(cmd.Args, " "), err, buf.String())
			}
		}
	}

	// Redirect the current module to its current source, espcially for directory packge paths.
	if origModPath != "" {
		// A local module might not be go-gettable. Rewrite go.mod to add a dummy require.
		{
			goModContent, err := os.ReadFile(filepath.Join(work, "go.mod"))
			if err != nil {
				return "", nil, err
			}
			mod, err := modfile.Parse("go.mod", goModContent, nil)
			if err != nil {
				return "", nil, err
			}
			// The version number is a dummy. This package will be redirected by the replace directive so the version doesn't matter.
			if err := mod.AddRequire(origModPath, "v0.0.0"); err != nil {
				return "", nil, err
			}
			newGoModContent, err := mod.Format()
			if err != nil {
				return "", nil, err
			}
			if err := os.WriteFile(filepath.Join(work, "go.mod"), newGoModContent, 0644); err != nil {
				return "", nil, err
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
				return "", nil, fmt.Errorf("uwagaki: '%s' failed: %w\n%s", strings.Join(cmd.Args, " "), err, buf.String())
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
					return "", nil, fmt.Errorf("uwagaki: '%s' failed: %w\n%s", strings.Join(cmd.Args, " "), err, buf.String())
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
					return "", nil, fmt.Errorf("uwagaki: '%s' failed: %w\n%s", strings.Join(cmd.Args, " "), err, buf.String())
				}
				modFilepath = strings.TrimSpace(string(out))
			}

			if err := replace(work, replacedModDir, r.Mod, modFilepath); err != nil {
				return "", nil, err
			}

			modVisited[r.Mod] = struct{}{}
		}

		dst := filepath.Join(replacedModDir, filepath.FromSlash(r.Mod), filepath.FromSlash(r.Path))
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			return "", nil, err
		}
		// Remove the file once if exists. The file is a hard link and the orignal file must not be affected.
		if err := os.Remove(dst); err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", nil, err
		}
		if err := os.WriteFile(dst, r.Content, 0644); err != nil {
			return "", nil, err
		}
	}

	if len(paths) == 0 {
		paths = []string{"."}
	}

	var currentModPath string
	if currentGoMod != "" {
		cmd := exec.Command("go", "list", "-m", "-f", "{{.Path}}")
		out, err := cmd.Output()
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				return "", nil, fmt.Errorf("uwagaki: '%s' failed: %w\n%s", strings.Join(cmd.Args, " "), ee, ee.Stderr)
			}
			return "", nil, err
		}
		currentModPath = strings.TrimSpace(string(out))
	}

	newPaths = make([]string, len(paths))
	for i, pkg := range paths {
		if !modfile.IsDirectoryPath(pkg) {
			newPaths[i] = pkg
			continue
		}

		abs, err := filepath.Abs(pkg)
		if err != nil {
			return "", nil, err
		}

		if currentGoMod == "" {
			newPaths[i] = abs
			continue
		}

		rel, err := filepath.Rel(filepath.Dir(currentGoMod), abs)
		if err != nil {
			return "", nil, err
		}
		newPaths[i] = path.Join(currentModPath, filepath.ToSlash(rel))
	}

	return work, newPaths, nil
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
		if err := filepath.WalkDir(moduleSrcFilepath, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(moduleSrcFilepath, path)
			if err != nil {
				return err
			}
			if d.IsDir() {
				if rel == ".git" {
					return filepath.SkipDir
				}
				return nil
			}
			dstPath := filepath.Join(dst, rel)
			if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
				return err
			}

			// Copy the file.
			// Symbolic links don't work for embedding. Hard links don't work between different file systems.
			out, err := os.Create(dstPath)
			if err != nil {
				return err
			}
			defer out.Close()

			in, err := os.Open(path)
			if err != nil {
				return err
			}
			defer in.Close()

			if _, err := io.Copy(out, in); err != nil {
				return err
			}
			return nil
		}); err != nil {
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
