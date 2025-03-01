// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2025 Hajime Hoshi

package uwagaki_test

import (
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

	tempraryMainGo []byte
	expectedOutput string
}

func mustReadFile(path string) []byte {
	b, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	return b
}

var testCases = []testCase{
	{
		name: "overwrite external module",

		wd:    ".",
		paths: []string{"golang.org/x/text/language@v0.22.0"},
		replaceItms: []uwagaki.ReplaceItem{
			{
				Mod:     "golang.org/x/text",
				Path:    "language/additional_file_by_uwagaki.go",
				Content: mustReadFile("./testdata/overwrite_external/additional_file_by_uwagaki.go"),
			},
		},
		expectedPaths:  []string{"golang.org/x/text/language@v0.22.0"},
		tempraryMainGo: mustReadFile("./testdata/overwrite_external/main.go"),
		expectedOutput: "Hello, Uwagaki!",
	},
	{
		name: "overwrite external module at temporary directory",

		wd:    os.TempDir(),
		paths: []string{"golang.org/x/text/language@v0.22.0"},
		replaceItms: []uwagaki.ReplaceItem{
			{
				Mod:     "golang.org/x/text",
				Path:    "language/additional_file_by_uwagaki.go",
				Content: mustReadFile("./testdata/overwrite_external/additional_file_by_uwagaki.go"),
			},
		},
		expectedPaths:  []string{"golang.org/x/text/language@v0.22.0"},
		tempraryMainGo: mustReadFile("./testdata/overwrite_external/main.go"),
		expectedOutput: "Hello, Uwagaki!",
	},
	{
		name: "overwrite relative path module",

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
		expectedPaths:  []string{"github.com/hajimehoshi/uwagaki/internal/testpkg"},
		tempraryMainGo: mustReadFile("./testdata/overwrite_relative/main.go"),
		expectedOutput: "Foo is called\nFoo2 is called",
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
}

func TestCreateEnvironment(t *testing.T) {
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

			if len(tc.tempraryMainGo) > 0 {
				if err := os.WriteFile(filepath.Join(dir, "main.go"), tc.tempraryMainGo, 0644); err != nil {
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

				if got, want := strings.TrimSpace(string(out)), "Overwritten Foo is called"; got != want {
					t.Errorf("got: %s, want: %s", got, want)
				}
			}
		})
	}
}
