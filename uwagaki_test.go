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

func TestCreateEnvironment(t *testing.T) {
	tmpDir := os.TempDir()

	for _, wd := range []string{".", tmpDir} {
		t.Run("wd="+wd, func(t *testing.T) {
			origWd, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chdir(wd); err != nil {
				t.Fatal(err)
			}
			defer os.Chdir(origWd)

			dir, paths, err := uwagaki.CreateEnvironment([]string{"github.com/hajimehoshi/ebiten/v2/examples/rotate@v2.8.6"}, []uwagaki.ReplaceItem{
				{
					Mod:  "github.com/hajimehoshi/ebiten/v2",
					Path: "additional_file_by_uwagaki.go",
					Content: []byte(`package ebiten

import (
	"fmt"
)

func AdditionalFuncByUwagaki() {
	fmt.Println("Hello, Uwagaki!")
}
`),
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(dir)

			if got, want := paths, []string{"github.com/hajimehoshi/ebiten/v2/examples/rotate@v2.8.6"}; !slices.Equal(got, want) {
				t.Errorf("got: %v, want: %v", got, want)
			}

			// Putting main.go in the created directory is not a usual way. This is just for testing.
			if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(`package main

import (
	"github.com/hajimehoshi/ebiten/v2"
)

func main() {
	ebiten.AdditionalFuncByUwagaki()
}
`), 0644); err != nil {
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

			if got, want := strings.TrimSpace(string(out)), "Hello, Uwagaki!"; got != want {
				t.Errorf("got: %s, want: %s", got, want)
			}
		})
	}
}

func TestCreateEnvironmentWithDirectoryPath(t *testing.T) {
	dir, paths, err := uwagaki.CreateEnvironment([]string{"./internal/testpkg"}, []uwagaki.ReplaceItem{
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
	})
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	if got, want := paths, []string{"github.com/hajimehoshi/uwagaki/internal/testpkg"}; !slices.Equal(got, want) {
		t.Errorf("got: %v, want: %v", got, want)
	}

	// Putting main.go in the created directory is not a usual way. This is just for testing.
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(`package main

import (
	"github.com/hajimehoshi/uwagaki"
)

func main() {
	uwagaki.Foo()
	uwagaki.Foo2()
}
`), 0644); err != nil {
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

	if got, want := strings.TrimSpace(string(out)), "Foo is called\nFoo2 is called"; got != want {
		t.Errorf("got: %s, want: %s", got, want)
	}
}

func TestCreateEnvironmentWithDirectoryPathWithRelativePath(t *testing.T) {
	if err := os.Chdir("./internal"); err != nil {
		t.Fatal(err)
	}

	dir, paths, err := uwagaki.CreateEnvironment([]string{"./testmainpkg"}, []uwagaki.ReplaceItem{
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
	})
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	if got, want := paths, []string{"github.com/hajimehoshi/uwagaki/internal/testmainpkg"}; !slices.Equal(got, want) {
		t.Errorf("got: %v, want: %v", got, want)
	}

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
