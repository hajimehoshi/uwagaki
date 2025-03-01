// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2025 Hajime Hoshi

package testpkg

import (
	"fmt"
)

func Foo() {
	fmt.Println("Overwritten Foo is called")
}
