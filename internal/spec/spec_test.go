// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package spec_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"go.dokimi.dev/mutate/internal/spec"
)

// vendored is the directory of the vendored definition.
var vendored = filepath.Join("..", "..", "conformance", "spec")

// strict decodes the file name of this package into v and fails t when the
// file names a field that v's type lacks.
func strict(t *testing.T, name string, v any) {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}

func TestSpec(t *testing.T) {
	t.Parallel()
	t.Run("the copies", func(t *testing.T) {
		t.Parallel()
		t.Run("are the files of the vendored definition", func(t *testing.T) {
			t.Parallel()
			for _, name := range []string{"VERSION", "catalogue.json", "protocol.json", "overlay.json"} {
				ours, err := os.ReadFile(name)
				if err != nil {
					t.Fatal(err)
				}
				theirs, err := os.ReadFile(filepath.Join(vendored, name))
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(ours, theirs) {
					t.Errorf("%s differs from %s, so make spec-sync has not run", name, filepath.Join(vendored, name))
				}
			}
		})
	})
	t.Run("Load", func(t *testing.T) {
		t.Parallel()
		t.Run("states every field of the copied files", func(t *testing.T) {
			t.Parallel()
			var want spec.Definition
			strict(t, "catalogue.json", &want.Catalogue)
			strict(t, "protocol.json", &want.Protocol)
			strict(t, "overlay.json", &want.Overlay)
			version, err := os.ReadFile("VERSION")
			if err != nil {
				t.Fatal(err)
			}
			want.Version = strings.TrimSpace(string(version))
			if got := spec.Load(); !reflect.DeepEqual(got, want) {
				t.Errorf("Load() = %+v, want %+v", got, want)
			}
		})
		t.Run("returns a semantic version", func(t *testing.T) {
			t.Parallel()
			if v := spec.Load().Version; !regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(v) {
				t.Errorf("Version = %q, want major.minor.patch", v)
			}
		})
		t.Run("returns the overlay's semantic version", func(t *testing.T) {
			t.Parallel()
			if v := spec.Load().Overlay.Version; !regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(v) {
				t.Errorf("Overlay.Version = %q, want major.minor.patch", v)
			}
		})
	})
	t.Run("Families.Calls", func(t *testing.T) {
		t.Parallel()
		t.Run("returns the overlay's list of each family of the catalogue but capacity", func(t *testing.T) {
			t.Parallel()
			d := spec.Load()
			data, err := os.ReadFile("overlay.json")
			if err != nil {
				t.Fatal(err)
			}
			var overlay struct {
				Families map[string]json.RawMessage `json:"families"`
			}
			if err := json.Unmarshal(data, &overlay); err != nil {
				t.Fatal(err)
			}
			want := map[string][]string{}
			for _, family := range d.Catalogue.Families {
				if family.ID == "capacity" {
					continue
				}
				var names []string
				if err := json.Unmarshal(overlay.Families[family.ID], &names); err != nil {
					t.Fatalf("family %s: %v", family.ID, err)
				}
				want[family.ID] = names
			}
			if got := d.Overlay.Families.Calls(); !reflect.DeepEqual(got, want) {
				t.Errorf("Calls() = %v, want %v", got, want)
			}
		})
	})
}
