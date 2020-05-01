package gen

import (
	"fmt"
	"io"

	"github.com/pulumi/pulumi/sdk/v2/go/common/util/contract"

	"github.com/pulumi/tf2pulumi/il"
)

// HILGenerator is an interface that
type HILGenerator interface {
	// GenArithmetic generates code for the indicated arithmetic node to the given writer.
	GenArithmetic(w io.Writer, v *il.BoundArithmetic)
	// GenCall generates code for the indicated call node to the given writer.
	GenCall(w io.Writer, v *il.BoundCall)
	// GenConditional generates code for the indicated conditional node to the given writer.
	GenConditional(w io.Writer, v *il.BoundConditional)
	// GenError generates code for the indicated error node to the given writer.
	GenError(w io.Writer, v *il.BoundError)
	// GenIndex generates code for the indicated index node to the given writer.
	GenIndex(w io.Writer, v *il.BoundIndex)
	// GenListProperty generates code for the indicated list property to the given writer.
	GenListProperty(w io.Writer, v *il.BoundListProperty)
	// GenLiteral generates code for the indicated literal node to the given writer.
	GenLiteral(w io.Writer, v *il.BoundLiteral)
	// GenMapProperty generates code for the indicated map property to the given writer.
	GenMapProperty(w io.Writer, v *il.BoundMapProperty)
	// GenOutput generates code for the indicated output node to the given writer.
	GenOutput(w io.Writer, v *il.BoundOutput)
	// GenPropertyValue generates code for the indicated property value node to the given writer.
	GenPropertyValue(w io.Writer, v *il.BoundPropertyValue)
	// GenVariableAccess generates code for the indicated variable access node to the given writer.
	GenVariableAccess(w io.Writer, v *il.BoundVariableAccess)
}

// Emitter is a convenience type that implements a number of common utilities used to emit source code. It implements
// the io.Writer interface.
type Emitter struct {
	// The current indent level as a string.
	Indent string

	// The HILGenerator to use in {G,Fg}en{,f}
	g HILGenerator
	// The writer to output to.
	w io.Writer
}

// NewEmitter creates a new emitter targeting the given io.Writer that will use the given HILGenerator when generating
// code.
func NewEmitter(w io.Writer, g HILGenerator) *Emitter {
	return &Emitter{w: w, g: g}
}

// Write writes the given bytes to the emitter's destination.
func (e *Emitter) Write(b []byte) (int, error) {
	return e.w.Write(b)
}

// indented bumps the current indentation level, invokes the given function, and then resets the indentation level to
// its prior value.
func (e *Emitter) Indented(f func()) {
	e.Indent += "    "
	f()
	e.Indent = e.Indent[:len(e.Indent)-4]
}

// Print prints one or more values to the generator's output stream.
func (e *Emitter) Print(a ...interface{}) {
	_, err := fmt.Fprint(e.w, a...)
	contract.IgnoreError(err)
}

// Println prints one or more values to the generator's output stream, followed by a newline.
func (e *Emitter) Println(a ...interface{}) {
	e.Print(a...)
	e.Print("\n")
}

// Printf prints a formatted message to the generator's output stream.
func (e *Emitter) Printf(format string, a ...interface{}) {
	_, err := fmt.Fprintf(e.w, format, a...)
	contract.IgnoreError(err)
}

// Gen is shorthand for Fgen(e, vs...).
func (e *Emitter) Gen(vs ...interface{}) {
	e.Fgen(e.w, vs...)
}

// Fgen generates code for a list of strings and expression trees. The former are written directly to the destination;
// the latter are recursively generated using the appropriate gen* functions.
func (e *Emitter) Fgen(w io.Writer, vs ...interface{}) {
	for _, v := range vs {
		switch v := v.(type) {
		case string:
			_, err := fmt.Fprint(w, v)
			contract.IgnoreError(err)
		case *il.BoundArithmetic:
			e.g.GenArithmetic(w, v)
		case *il.BoundCall:
			e.g.GenCall(w, v)
		case *il.BoundConditional:
			e.g.GenConditional(w, v)
		case *il.BoundIndex:
			e.g.GenIndex(w, v)
		case *il.BoundLiteral:
			e.g.GenLiteral(w, v)
		case *il.BoundOutput:
			e.g.GenOutput(w, v)
		case *il.BoundPropertyValue:
			e.g.GenPropertyValue(w, v)
		case *il.BoundVariableAccess:
			e.g.GenVariableAccess(w, v)
		case *il.BoundListProperty:
			e.g.GenListProperty(w, v)
		case *il.BoundMapProperty:
			e.g.GenMapProperty(w, v)
		case *il.BoundError:
			e.g.GenError(w, v)
		default:
			contract.Failf("unexpected type in gen: %T", v)
		}
	}
}

// Genf is shorthand for Fgenf(e, format, args...)
func (e *Emitter) Genf(format string, args ...interface{}) {
	e.Fgenf(e.w, format, args...)
}

// Fgenf generates code using a format string and its arguments. Any arguments that are BoundNode values are wrapped in
// a FormatFunc that calls the appropriate recursive generation function. This allows for the composition of standard
// format strings with expression/property code gen (e.e. `e.genf(w, ".apply(__arg0 => %v)", then)`, where `then` is
// an expression tree).
func (e *Emitter) Fgenf(w io.Writer, format string, args ...interface{}) {
	for i := range args {
		if node, ok := args[i].(il.BoundNode); ok {
			args[i] = FormatFunc(func(f fmt.State, c rune) { e.Fgen(f, node) })
		}
	}
	fmt.Fprintf(w, format, args...)
}
