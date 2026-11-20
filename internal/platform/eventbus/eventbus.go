// Package eventbus defines the minimal event emission contract shared by the
// application services. It lets lower layers publish events without importing
// the desktop runtime; the composition root supplies the real implementation.
package eventbus

// Bus publishes a named event with a typed payload.
type Bus interface {
	Emit(name string, payload any)
}

// Func adapts a plain function to the Bus interface. A nil Func is valid and
// silently drops events, which keeps headless tests and early startup safe.
type Func func(name string, payload any)

// Emit calls f when it is not nil.
func (f Func) Emit(name string, payload any) {
	if f != nil {
		f(name, payload)
	}
}
