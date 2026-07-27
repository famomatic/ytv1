package client

// Register the built-in non-YouTube sources. Importing a source package for its
// init() side effect adds it to the source registry, so client.New picks it up
// via source.Build. Adding a new site is a matter of adding one blank import
// here (and the package under internal/source/<name>).
import (
	_ "github.com/famomatic/ytv1/internal/source/soop"
)
