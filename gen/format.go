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
