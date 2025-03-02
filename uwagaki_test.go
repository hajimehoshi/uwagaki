// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2025 Hajime Hoshi

package uwagaki_test

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/hajimehoshi/uwagaki"
)

type testCase struct {
	name string

	wd            string
	paths         []string
	replaceItms   []uwagaki.ReplaceItem
	expectedPaths []string

	temporaryMainGo []byte
	expectedOutput  string
}

func mustReadFile(path string) []byte {
	b, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	return b
}

func copyFSWithoutDotGit(dst, src string) error {
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	if err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
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
	return nil
}

func TestCreateEnvironment(t *testing.T) {
	tmp, err := os.MkdirTemp("", "")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmp)

	tmpWithLocalGoMod, err := os.MkdirTemp("", "")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpWithLocalGoMod)

	tmpWithRealGoMod, err := os.MkdirTemp("", "")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpWithRealGoMod)

	{
		cmd := exec.Command("go", "mod", "init", "foo")
		cmd.Stderr = os.Stderr
		cmd.Dir = tmpWithLocalGoMod
		if err := cmd.Run(); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(tmpWithLocalGoMod, "main.go"), []byte(`package main

import "fmt"

func main() {
	fmt.Println("Package foo's main is called")
}
`), 0644); err != nil {
			t.Fatal(err)
		}
	}
	{
		cmd := exec.Command("go", "get", "github.com/hajimehoshi/uwagaki")
		cmd.Stderr = os.Stderr
		cmd.Dir = tmpWithLocalGoMod
		if err := cmd.Run(); err != nil {
			t.Fatal(err)
		}
	}
	// Prepare replaced module.
	{
		wd, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		replaced := filepath.Join(tmpWithLocalGoMod, "_uwagaki")
		if err := copyFSWithoutDotGit(replaced, wd); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(replaced, "internal", "testpkg", "foo.go"), []byte(`package testpkg

import "fmt"

func Foo() {
	fmt.Println("Replaced Foo is called")
}
`), 0644); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("go", "mod", "edit", "-replace=github.com/hajimehoshi/uwagaki=."+string(filepath.Separator)+"_uwagaki")
		cmd.Stderr = os.Stderr
		cmd.Dir = tmpWithLocalGoMod
		if err := cmd.Run(); err != nil {
			t.Fatal(err)
		}
	}

	{
		cmd := exec.Command("git", "clone", "--depth=1", "https://go.googlesource.com/tools")
		cmd.Stderr = os.Stderr
		cmd.Dir = tmpWithRealGoMod
		if err := cmd.Run(); err != nil {
			t.Fatal(err)
		}

		// Modify cmd/stringer/main.go for testing.
		if err := os.RemoveAll(filepath.Join(tmpWithRealGoMod, "tools", "cmd", "stringer")); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(tmpWithRealGoMod, "tools", "cmd", "stringer"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(tmpWithRealGoMod, "tools", "cmd", "stringer", "main.go"), mustReadFile("./testdata/stringer/main.go"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	var testCases = []testCase{
		{
			name:  "overwrite external module",
			wd:    ".",
			paths: []string{"golang.org/x/text/language@v0.22.0"},
			replaceItms: []uwagaki.ReplaceItem{
				{
					Mod:     "golang.org/x/text",
					Path:    "language/additional_file_by_uwagaki.go",
					Content: mustReadFile("./testdata/overwrite_external/additional_file_by_uwagaki.go"),
				},
			},
			expectedPaths:   []string{"golang.org/x/text/language@v0.22.0"},
			temporaryMainGo: mustReadFile("./testdata/overwrite_external/main.go"),
			expectedOutput:  "Hello, Uwagaki!",
		},
		{
			name:  "overwrite external module at temporary directory",
			wd:    tmp,
			paths: []string{"golang.org/x/text/language@v0.22.0"},
			replaceItms: []uwagaki.ReplaceItem{
				{
					Mod:     "golang.org/x/text",
					Path:    "language/additional_file_by_uwagaki.go",
					Content: mustReadFile("./testdata/overwrite_external/additional_file_by_uwagaki.go"),
				},
			},
			expectedPaths:   []string{"golang.org/x/text/language@v0.22.0"},
			temporaryMainGo: mustReadFile("./testdata/overwrite_external/main.go"),
			expectedOutput:  "Hello, Uwagaki!",
		},
		{
			name:  "overwrite relative path module",
			wd:    ".",
			paths: []string{"./internal/testpkg"},
			replaceItms: []uwagaki.ReplaceItem{
				{
					Mod:     "github.com/hajimehoshi/uwagaki",
					Path:    "foo.go",
					Content: mustReadFile("./testdata/overwrite_relative/uwagaki/foo.go"),
				},
				{
					Mod:     "github.com/hajimehoshi/uwagaki",
					Path:    "internal/testpkg/foo2.go",
					Content: mustReadFile("./testdata/overwrite_relative/testpkg/foo2.go"),
				},
			},
			expectedPaths:   []string{"github.com/hajimehoshi/uwagaki/internal/testpkg"},
			temporaryMainGo: mustReadFile("./testdata/overwrite_relative/main.go"),
			expectedOutput:  "Foo is called\nFoo2 is called",
		},
		{
			name:  "overwrite relative path main module",
			wd:    "./internal",
			paths: []string{"./testmainpkg"},
			replaceItms: []uwagaki.ReplaceItem{
				{
					Mod:     "github.com/hajimehoshi/uwagaki",
					Path:    "internal/testpkg/foo.go",
					Content: mustReadFile("./testdata/overwrite_relative/testpkg/foo.go"),
				},
			},
			expectedPaths:  []string{"github.com/hajimehoshi/uwagaki/internal/testmainpkg"},
			expectedOutput: "Overwritten Foo is called",
		},
		{
			name:           "local go.mod with absolute path",
			wd:             tmpWithLocalGoMod,
			paths:          []string{"github.com/hajimehoshi/uwagaki/internal/testmainpkg"},
			replaceItms:    nil,
			expectedPaths:  []string{"github.com/hajimehoshi/uwagaki/internal/testmainpkg"},
			expectedOutput: "Replaced Foo is called",
		},
		{
			name:           "local go.mod with relative path",
			wd:             tmpWithLocalGoMod,
			paths:          []string{"."},
			replaceItms:    nil,
			expectedPaths:  []string{"foo"},
			expectedOutput: "Package foo's main is called",
		},
		{
			name:           "real go.mod with absolute path",
			wd:             filepath.Join(tmpWithRealGoMod, "tools"),
			paths:          []string{"golang.org/x/tools/cmd/stringer"},
			replaceItms:    nil,
			expectedPaths:  []string{"golang.org/x/tools/cmd/stringer"},
			expectedOutput: "This is a new stringer",
		},
		{
			name:           "real go.mod with relative path",
			wd:             filepath.Join(tmpWithRealGoMod, "tools"),
			paths:          []string{"./cmd/stringer"},
			replaceItms:    nil,
			expectedPaths:  []string{"golang.org/x/tools/cmd/stringer"},
			expectedOutput: "This is a new stringer",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// TODO: Use t.Chdir after Go 1.24.
			origWd, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chdir(tc.wd); err != nil {
				t.Fatal(err)
			}
			defer os.Chdir(origWd)

			dir, paths, err := uwagaki.CreateEnvironment(tc.paths, tc.replaceItms)
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(dir)

			if got, want := paths, tc.expectedPaths; !slices.Equal(got, want) {
				t.Errorf("paths: got: %v, want: %v", got, want)
			}

			if len(tc.temporaryMainGo) > 0 {
				if err := os.WriteFile(filepath.Join(dir, "main.go"), tc.temporaryMainGo, 0644); err != nil {
					t.Fatal(err)
				}

				cmd := exec.Command("go", "run")
				cmd.Args = append(cmd.Args, "main.go")
				cmd.Dir = dir
				out, err := cmd.Output()
				if err != nil {
					if ee, ok := err.(*exec.ExitError); ok {
						t.Fatalf("exit status: %d\n%s", ee.ExitCode(), ee.Stderr)
					}
					t.Fatal(err)
				}

				if got, want := strings.TrimSpace(string(out)), tc.expectedOutput; got != want {
					t.Errorf("output: got: %s, want: %s", got, want)
				}
			} else {
				cmd := exec.Command("go", "run")
				cmd.Args = append(cmd.Args, paths...)
				cmd.Dir = dir
				out, err := cmd.Output()
				if err != nil {
					if ee, ok := err.(*exec.ExitError); ok {
						t.Fatalf("exit status: %d\n%s", ee.ExitCode(), ee.Stderr)
					}
					t.Fatal(err)
				}

				if got, want := strings.TrimSpace(string(out)), tc.expectedOutput; got != want {
					t.Errorf("got: %s, want: %s", got, want)
				}
			}
		})
	}
}
