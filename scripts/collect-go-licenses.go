//go:build ignore

// Collect notices from the modules actually linked into the native desktop.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type module struct{ Path, Version, Dir string }

func main() {
	if len(os.Args) != 2 {
		panic("expected output filename")
	}
	cmd := exec.Command("go", "list", "-deps", "-tags", "desktop", "-json", "./cmd/desktop")
	b, err := cmd.Output()
	if err != nil {
		panic(err)
	}
	modules := map[string]module{}
	dec := json.NewDecoder(bytes.NewReader(b))
	for {
		var p struct{ Module *module }
		err := dec.Decode(&p)
		if err == io.EOF {
			break
		}
		if err != nil {
			panic(err)
		}
		if p.Module != nil && p.Module.Version != "" {
			modules[p.Module.Path] = *p.Module
		}
	}
	keys := []string{}
	for key := range modules {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var out strings.Builder
	out.WriteString("Go desktop dependency licenses and notices\n\n")
	for _, key := range keys {
		m := modules[key]
		fmt.Fprintf(&out, "\n=== %s %s ===\nSource: https://pkg.go.dev/%s@%s\n", key, m.Version, key, m.Version)
		found := 0
		err := filepath.WalkDir(m.Dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			name := strings.ToLower(d.Name())
			stem := strings.TrimSuffix(name, filepath.Ext(name))
			if stem != "license" && stem != "licence" && stem != "copying" && stem != "notice" && stem != "ofl" {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(m.Dir, path)
			fmt.Fprintf(&out, "\n--- %s ---\n%s\n", rel, data)
			found++
			return nil
		})
		if err != nil {
			panic(err)
		}
		if found == 0 {
			panic("license missing for " + key)
		}
	}
	if err := os.WriteFile(os.Args[1], []byte(out.String()), 0644); err != nil {
		panic(err)
	}
}
