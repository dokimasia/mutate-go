// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package spec

import (
	"embed"
	"encoding/json"
	"strings"
	"sync"
)

// The copies of the vendored definition's files that the package embeds.
const (
	VersionFile   = "VERSION"
	CatalogueFile = "catalogue.json"
	ProtocolFile  = "protocol.json"
	OverlayFile   = "overlay.json"
)

// files contains the copies of the vendored definition's files that the
// engine reads when it runs.
//
//go:embed VERSION catalogue.json protocol.json overlay.json
var files embed.FS

// Definition is the part of the vendored definition that the engine reads
// when it runs.
type Definition struct {
	// Version is the catalogue's version, which every record states.
	Version   string
	Catalogue Catalogue
	Protocol  Protocol
	Overlay   Overlay
}

// Load returns the definition that the engine reads when it runs. The first
// call decodes the embedded files, and every later call returns the same
// definition.
//
// The embed directive includes every file that Load reads, and the tests of
// this package decode each file strictly, so Load ignores the errors of the
// read and the decode.
func Load() Definition {
	return definition()
}

// definition decodes the embedded files on its first call, once per process.
var definition = sync.OnceValue(func() Definition {
	var d Definition
	d.Version = strings.TrimSpace(string(read(VersionFile)))
	_ = json.Unmarshal(read(CatalogueFile), &d.Catalogue)
	_ = json.Unmarshal(read(ProtocolFile), &d.Protocol)
	_ = json.Unmarshal(read(OverlayFile), &d.Overlay)
	return d
})

// read returns the content of the embedded file name.
func read(name string) []byte {
	data, _ := files.ReadFile(name)
	return data
}
