// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2025 Hajime Hoshi

package uwagaki_test

import (
	"os"
	"os/exec"

	"github.com/hajimehoshi/uwagaki"
)

func ExampleCreateEnvironment() {
	dir, pkgs, err := uwagaki.CreateEnvironment([]string{"github.com/hajimehoshi/uwagaki/internal/testmainpkg"}, []uwagaki.ReplaceItem{
		{
			Mod:  "github.com/hajimehoshi/uwagaki",
			Path: "internal/testpkg/foo.go",
			Content: []byte(`package testpkg

import "fmt"

func Foo() {
	fmt.Println("Overwritten Foo is called")
}`),
		},
	})
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	cmd := exec.Command("go", "run")
	cmd.Args = append(cmd.Args, pkgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		panic(err)
	}

	// Output: Overwritten Foo is called
}
