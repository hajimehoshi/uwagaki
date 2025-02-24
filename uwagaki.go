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
	"path/filepath"
	"strings"
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

	if currentGoMod != "" {
		// Copy the current go.mod and go.sum to the work directory.
		if err := copyFile(filepath.Join(work, "go.mod"), currentGoMod); err != nil {
			return "", err
		}
	} else {
		// go mod init
		var buf bytes.Buffer
		cmd := exec.Command("go", "mod", "init", "uwakagi")
		cmd.Stderr = &buf
		cmd.Dir = work
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("uwagaki: '%s' failed: %w\n%s", strings.Join(cmd.Args, " "), err, buf.String())
		}
	}

	// go get
	{
		var buf bytes.Buffer
		cmd := exec.Command("go", "get")
		cmd.Args = append(cmd.Args, pkgs...)
		cmd.Stderr = &buf
		cmd.Dir = work
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("uwagaki: '%s' failed: %w\n%s", strings.Join(cmd.Args, " "), err, buf.String())
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

			// Copy files.
			dst := filepath.Join(replacedModDir, filepath.FromSlash(r.Mod))
			f, err := os.Stat(dst)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return "", err
			}
			if err == nil && !f.IsDir() {
				return "", fmt.Errorf("uwagaki: %s is not a directory", dst)
			}
			if errors.Is(err, os.ErrNotExist) {
				// TODO: Use symbolic linkes instead of copying files.
				if err := os.CopyFS(dst, os.DirFS(modFilepath)); err != nil {
					return "", err
				}
			}

			// go mod edit
			{
				dstRel := "." + string(filepath.Separator) + filepath.Join("mod", filepath.FromSlash(r.Mod))
				var buf bytes.Buffer
				// TODO: What if the file path includes a space?
				cmd := exec.Command("go", "mod", "edit", "-replace", r.Mod+"="+dstRel)
				cmd.Stderr = &buf
				cmd.Dir = work
				if err := cmd.Run(); err != nil {
					return "", fmt.Errorf("uwagaki: '%s' failed: %w\n%s", strings.Join(cmd.Args, " "), err, buf.String())
				}
			}

			modVisited[r.Mod] = struct{}{}
		}

		dst := filepath.Join(replacedModDir, filepath.FromSlash(r.Mod), filepath.FromSlash(r.Path))
		if err := os.WriteFile(dst, r.Content, 0644); err != nil {
			return "", err
		}
	}

	return work, nil
}

func copyFile(dst, src string) error {
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return nil
}
