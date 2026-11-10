//go:build loong64

package monitor

// The previous Agent deliberately did not collect built-in system metrics on
// loong64. Keep that behavior rather than silently changing the contract.
func Sources() []string { return nil }
