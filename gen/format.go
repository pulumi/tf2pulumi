package gen

import (
	"fmt"
)

type FormatFunc func(f fmt.State, c rune)

func (p FormatFunc) Format(f fmt.State, c rune) {
	p(f, c)
}
