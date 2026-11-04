package mcp

import (
	"context"
	"fmt"
)

// OpenAvailable keeps working servers usable when an unrelated server is unavailable.
// Each server is accepted or rejected as a whole, with the same aggregate limits as Open.
func OpenAvailable(ctx context.Context, servers []Resolved) (*Runtime, []string, error) {
	combined := &Runtime{targets: map[string]target{}}
	var unavailable []string
	totalSchema := 0
	for _, server := range servers {
		if err := ctx.Err(); err != nil {
			combined.Close()
			return nil, nil, err
		}
		one, err := Open(ctx, []Resolved{server})
		if err != nil {
			unavailable = append(unavailable, server.Name)
			continue
		}
		nextBytes := 0
		for _, fn := range one.functions {
			nextBytes += len(fn.Parameters)
		}
		if len(combined.functions)+len(one.functions) > maxRuntimeTools || totalSchema+nextBytes > maxToolSchemasTotal {
			one.Close()
			unavailable = append(unavailable, server.Name)
			continue
		}
		for _, fn := range one.functions {
			target := one.targets[fn.Name]
			fn.Name = fmt.Sprintf("mcp_tool_%03d", len(combined.functions)+1)
			combined.functions = append(combined.functions, fn)
			combined.targets[fn.Name] = target
		}
		totalSchema += nextBytes
		combined.sessions = append(combined.sessions, one.sessions...)
		combined.cleanups = append(combined.cleanups, one.cleanups...)
	}
	return combined, unavailable, nil
}
