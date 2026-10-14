package input

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// Device metadata remains inside the input package and is never logged.
func command(ctx context.Context, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	data, err := exec.CommandContext(ctx, name, args...).Output()
	return strings.TrimSpace(string(data)), err
}
