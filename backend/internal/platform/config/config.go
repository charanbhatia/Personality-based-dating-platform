// Package config re-exports the application config so Person C packages that
// import internal/platform/config keep compiling after the merge.
package config

import (
	"fmt"
	"os"

	appconfig "github.com/bits-assignment/dating-platform/backend/internal/config"
)

type Config = appconfig.Config

// Load returns the merged config. Person C callers do not handle errors, so a
// validation failure exits the process the same way a missing DATABASE_URL did
// before the merge.
func Load() *Config {
	c, err := appconfig.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}
	return c
}
