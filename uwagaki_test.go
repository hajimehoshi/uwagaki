// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2025 Hajime Hoshi

package uwagaki_test

import (
	"os"
	"os/exec"
	"path/filepath"
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

			dir, err := uwagaki.CreateEnvironment([]string{"github.com/hajimehoshi/ebiten/v2/examples/rotate@v2.8.6"}, []uwagaki.ReplaceItem{
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

			cmd := exec.Command("go", "run", "main.go")
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
