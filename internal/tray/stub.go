//go:build !tray

package tray

import "context"

// Run is a no-op when built without the tray tag.
func Run(_ context.Context, _ string) {}
