// Package config is a compatibility shim over internal/platform/config so that
// existing importers keep working while the platform package owns the real
// configuration surface.
package config

import platformconfig "github.com/bits-assignment/dating-platform/backend/internal/platform/config"

type Config = platformconfig.Config

func Load() *Config { return platformconfig.Load() }
