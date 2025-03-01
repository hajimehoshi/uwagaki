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

	tempraryMainGo string
	expectedOutput string
}

var testCases = []testCase{
	{
		name: "overwrite external module",

		wd:    ".",
		paths: []string{"golang.org/x/text/language@v0.22.0"},
		replaceItms: []uwagaki.ReplaceItem{
			{
				Mod:  "golang.org/x/text",
				Path: "language/additional_file_by_uwagaki.go",
				Content: []byte(`package language

import (
	"fmt"
)

func AdditionalFuncByUwagaki() {
	fmt.Println("Hello, Uwagaki!")
}
`),
			},
		},
		expectedPaths: []string{"golang.org/x/text/language@v0.22.0"},
		tempraryMainGo: `package main

import (
	"golang.org/x/text/language"
)

func main() {
	language.AdditionalFuncByUwagaki()
}
`,
		expectedOutput: "Hello, Uwagaki!",
	},
	{
		name: "overwrite external module at temporary directory",

		wd:    os.TempDir(),
		paths: []string{"golang.org/x/text/language@v0.22.0"},
		replaceItms: []uwagaki.ReplaceItem{
			{
				Mod:  "golang.org/x/text",
				Path: "language/additional_file_by_uwagaki.go",
				Content: []byte(`package language

import (
	"fmt"
)

func AdditionalFuncByUwagaki() {
	fmt.Println("Hello, Uwagaki!")
}
`),
			},
		},
		expectedPaths: []string{"golang.org/x/text/language@v0.22.0"},
		tempraryMainGo: `package main

import (
	"golang.org/x/text/language"
)

func main() {
	language.AdditionalFuncByUwagaki()
}
`,
		expectedOutput: "Hello, Uwagaki!",
	},
	{
		name: "overwrite relative path module",

		wd:    ".",
		paths: []string{"./internal/testpkg"},
		replaceItms: []uwagaki.ReplaceItem{
			{
				Mod:  "github.com/hajimehoshi/uwagaki",
				Path: "foo.go",
				Content: []byte(`package uwagaki

import (
	"github.com/hajimehoshi/uwagaki/internal/testpkg"
)

func Foo() {
	testpkg.Foo()
}

func Foo2() {
	testpkg.Foo2()
}
`),
			},
			{
				Mod:  "github.com/hajimehoshi/uwagaki",
				Path: "internal/testpkg/foo2.go",
				Content: []byte(`package testpkg

import (
	"fmt"
)

func Foo2() {
	fmt.Println("Foo2 is called")
}
`),
			},
		},
		expectedPaths: []string{"github.com/hajimehoshi/uwagaki/internal/testpkg"},
		tempraryMainGo: `package main

import (
	"github.com/hajimehoshi/uwagaki"
)

func main() {
	uwagaki.Foo()
	uwagaki.Foo2()
}`,
		expectedOutput: "Foo is called\nFoo2 is called",
	},
	{
		name:  "overwrite relative path main module",
		wd:    "./internal",
		paths: []string{"./testmainpkg"},
		replaceItms: []uwagaki.ReplaceItem{
			{
				Mod:  "github.com/hajimehoshi/uwagaki",
				Path: "internal/testpkg/foo.go",
				Content: []byte(`package testpkg

import (
	"fmt"
)

func Foo() {
	fmt.Println("Overwritten Foo is called")
}
`),
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

			if tc.tempraryMainGo != "" {
				if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(tc.tempraryMainGo), 0644); err != nil {
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
