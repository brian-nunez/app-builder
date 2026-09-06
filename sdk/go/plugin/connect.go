package plugin

import "fmt"

// Connectable reports whether an output port may feed an input port.
//
// This is the authority. The canvas carries its own copy so it can answer without
// a round trip, and both are held to sdk/contract/port-compatibility.json.
func Connectable(out, in Port) error {
	if out.Kind != in.Kind {
		return fmt.Errorf("incompatible connection types")
	}
	if out.ResourceType != in.ResourceType {
		return fmt.Errorf("incompatible connection types")
	}
	if out.Sensitive && !in.Sensitive {
		return fmt.Errorf("sensitive output requires a sensitive input")
	}
	if outType, ok := out.Schema["type"].(string); ok {
		if inType, ok := in.Schema["type"].(string); ok && outType != inType {
			return fmt.Errorf("incompatible data types")
		}
	}
	return nil
}

// AcceptsAnother reports whether an input port may take one more source given how
// many are already bound to it. Only a port declaring multiple joins its sources.
func AcceptsAnother(in Port, bound int) bool {
	return bound == 0 || in.Multiple
}
