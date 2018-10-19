// Copyright 2016-2018, Pulumi Corporation.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package gen

import (
	"fmt"
)

// FormatFunc is a function type that implements the fmt.Formatter interface. This can be used to conveniently
// implement this interface for types defined in other packages.
type FormatFunc func(f fmt.State, c rune)

// Format invokes the FormatFunc's underlying function.
func (p FormatFunc) Format(f fmt.State, c rune) {
	p(f, c)
}
