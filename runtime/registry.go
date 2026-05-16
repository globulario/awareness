package runtime

import "fmt"

// New returns the Adapter for the given name.
//
// The standalone module only knows "null". The services repository selects
// "globular" locally by constructing a GlobularAdapter directly and passing it
// wherever an Adapter is required — no global registry needed.
//
// If name is empty it defaults to "null".
// If name is unrecognised, New returns an actionable error; callers should
// surface it rather than silently falling back.
func New(name string) (Adapter, error) {
	switch name {
	case "", "null":
		return NullAdapter{}, nil
	default:
		return nil, fmt.Errorf("unknown runtime adapter %q: register it with the host application", name)
	}
}
