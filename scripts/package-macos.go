//go:build ignore

// Run on an Apple Silicon Mac: go run scripts/package-macos.go -version v0.3.0
package main

import (
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

type download struct{ Name, URL, SHA256, LicenseEntry string }
type lockfile struct {
	Version, Commit     string
	DarwinArm64, Source download
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func command(name string, args ...string) string {
	c := exec.Command(name, args...)
	b, err := c.CombinedOutput()
	if err != nil {
		panic(fmt.Sprintf("%s: %s: %v", name, b, err))
	}
	return strings.TrimSpace(string(b))
}
func digest(b []byte) string  { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
func read(path string) []byte { b, err := os.ReadFile(path); must(err); return b }
func write(path string, b []byte, mode os.FileMode) {
	must(os.MkdirAll(filepath.Dir(path), 0755))
	must(os.WriteFile(path, b, mode))
	must(os.Chmod(path, mode))
}
func verified(item download) string {
	path := filepath.Join(".release-cache", item.Name)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		fmt.Println("Downloading", item.Name)
		client := http.Client{Timeout: 5 * time.Minute}
		resp, err := client.Get(item.URL)
		must(err)
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			panic(resp.Status)
		}
		b, err := io.ReadAll(io.LimitReader(resp.Body, 256<<20))
		must(err)
		if digest(b) != item.SHA256 {
			panic("download checksum mismatch: " + item.Name)
		}
		write(path, b, 0644)
	}
	if digest(read(path)) != item.SHA256 {
		panic("cached checksum mismatch: " + item.Name)
	}
	return path
}

func main() {
	version := flag.String("version", "", "release version vX.Y.Z")
	flag.Parse()
	if !regexp.MustCompile(`^v\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$`).MatchString(*version) {
		panic("invalid version")
	}
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		panic("build this bundle on an Apple Silicon Mac")
	}
	if command("git", "status", "--porcelain") != "" {
		panic("commit source changes before packaging")
	}
	commit := command("git", "rev-parse", "HEAD")
	var lock lockfile
	must(json.Unmarshal(read("scripts/mihomo.lock.json"), &lock))
	name := "proxy-gateway-" + *version + "-macos-arm64"
	out := filepath.Join("dist", *version)
	stage := filepath.Join(out, name)
	if _, err := os.Stat(stage); !os.IsNotExist(err) {
		panic("output already exists: " + stage)
	}
	app := filepath.Join(stage, "Proxy Gateway.app")
	resources := filepath.Join(app, "Contents", "Resources")
	macos := filepath.Join(app, "Contents", "MacOS")
	must(os.MkdirAll(resources, 0755))
	must(os.MkdirAll(macos, 0755))
	sourcePath, binaryPath := verified(lock.Source), verified(lock.DarwinArm64)
	f, err := os.Open(binaryPath)
	must(err)
	gz, err := gzip.NewReader(f)
	must(err)
	b, err := io.ReadAll(gz)
	must(err)
	must(gz.Close())
	must(f.Close())
	write(filepath.Join(resources, "mihomo"), b, 0755)
	for _, build := range [][2]string{{filepath.Join(resources, "proxy-gateway"), "."}, {filepath.Join(macos, "ProxyGateway"), "./cmd/macos-launcher"}} {
		c := exec.Command("go", "build", "-trimpath", "-buildvcs=false", "-ldflags", "-s -w -X main.version="+*version+" -X main.commit="+commit, "-o", build[0], build[1])
		c.Env = append(os.Environ(), "GOOS=darwin", "GOARCH=arm64", "CGO_ENABLED=0", "GOARM64=v8.0")
		if b, err := c.CombinedOutput(); err != nil {
			panic(fmt.Sprintf("build: %s: %v", b, err))
		}
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleName</key><string>Proxy Gateway</string>
<key>CFBundleDisplayName</key><string>Proxy Gateway</string>
<key>CFBundleIdentifier</key><string>io.github.AkparAby.proxy-gateway</string>
<key>CFBundleExecutable</key><string>ProxyGateway</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>CFBundleShortVersionString</key><string>` + strings.TrimPrefix(*version, "v") + `</string>
<key>CFBundleVersion</key><string>` + strings.Split(strings.TrimPrefix(*version, "v"), "-")[0] + `</string>
<key>CFBundleIconFile</key><string>AppIcon</string>
<key>LSMinimumSystemVersion</key><string>12.0</string>
<key>LSUIElement</key><true/>
<key>NSHighResolutionCapable</key><true/>
</dict></plist>
`
	write(filepath.Join(app, "Contents", "Info.plist"), []byte(plist), 0644)
	command("/usr/bin/plutil", "-lint", filepath.Join(app, "Contents", "Info.plist"))
	iconset := filepath.Join(".release-cache", name+".iconset")
	must(os.MkdirAll(iconset, 0755))
	for _, size := range []int{16, 32, 128, 256, 512} {
		for _, scale := range []int{1, 2} {
			suffix := ""
			if scale == 2 {
				suffix = "@2x"
			}
			command("/usr/bin/sips", "-z", fmt.Sprint(size*scale), fmt.Sprint(size*scale), "winres/icon.png", "--out", filepath.Join(iconset, fmt.Sprintf("icon_%dx%d%s.png", size, size, suffix)))
		}
	}
	command("/usr/bin/iconutil", "-c", "icns", iconset, "-o", filepath.Join(resources, "AppIcon.icns"))
	for _, file := range []string{"README.md", "LICENSE", "THIRD_PARTY_NOTICES.md"} {
		write(filepath.Join(stage, file), read(file), 0644)
		write(filepath.Join(resources, file), read(file), 0644)
	}
	z, err := zip.OpenReader(sourcePath)
	must(err)
	found := false
	for _, file := range z.File {
		if file.Name != lock.Source.LicenseEntry {
			continue
		}
		r, err := file.Open()
		must(err)
		b, err := io.ReadAll(r)
		must(err)
		must(r.Close())
		write(filepath.Join(resources, "licenses", "Mihomo-GPL-3.0.txt"), b, 0644)
		found = true
	}
	must(z.Close())
	if !found {
		panic("Mihomo license missing")
	}
	write(filepath.Join(resources, "licenses", "Go-BSD.txt"), read(filepath.Join(command("go", "env", "GOROOT"), "LICENSE")), 0644)
	// Ad-hoc signing supplies a valid bundle seal; this is NOT Developer ID
	// signing or notarization and still requires Gatekeeper user approval.
	for _, path := range []string{filepath.Join(resources, "mihomo"), filepath.Join(resources, "proxy-gateway"), app} {
		command("/usr/bin/codesign", "--force", "--sign", "-", "--timestamp=none", path)
	}
	command("/usr/bin/codesign", "--verify", "--deep", "--strict", app)
	restore := "#!/bin/sh\nset -eu\nbase=$(CDPATH= cd -- \"$(dirname -- \"$0\")\" && pwd)\napp=\"$base/Proxy Gateway.app\"\nif [ ! -d \"$app\" ]; then app='/Applications/Proxy Gateway.app'; fi\nif [ ! -d \"$app\" ]; then app=\"$HOME/Applications/Proxy Gateway.app\"; fi\n\"$app/Contents/Resources/proxy-gateway\" --restore\nprintf '\\nPress Enter to close.'\nread -r answer\n"
	write(filepath.Join(stage, "Restore-Proxy.command"), []byte(restore), 0755)
	files := map[string]string{}
	must(filepath.WalkDir(stage, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(stage, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = digest(read(path))
		return nil
	}))
	manifest := map[string]any{"project": "https://github.com/AkparAby/proxy-gateway", "version": *version, "commit": commit, "platform": "darwin/arm64", "go": command("go", "version"), "builtAtUtc": time.Now().UTC().Format(time.RFC3339), "signing": "ad-hoc; not notarized", "files": files, "mihomo": map[string]string{"version": lock.Version, "commit": lock.Commit, "binaryUrl": lock.DarwinArm64.URL, "binaryArchiveSha256": lock.DarwinArm64.SHA256, "sourceUrl": "https://github.com/AkparAby/proxy-gateway/releases/download/" + *version + "/" + lock.Source.Name, "sourceArchiveSha256": lock.Source.SHA256}}
	b, err = json.MarshalIndent(manifest, "", "  ")
	must(err)
	write(filepath.Join(stage, "release-manifest.json"), append(b, '\n'), 0644)
	packagePath := filepath.Join(out, name+".zip")
	output, err := os.OpenFile(packagePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	must(err)
	zw := zip.NewWriter(output)
	must(filepath.WalkDir(stage, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		h, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(out, path)
		if err != nil {
			return err
		}
		h.Name = filepath.ToSlash(rel)
		h.Method = zip.Deflate
		h.SetMode(info.Mode()) // Preserve executable bits when Finder extracts the ZIP.
		w, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		_, err = w.Write(read(path))
		return err
	}))
	must(zw.Close())
	must(output.Close())
	write(filepath.Join(out, lock.Source.Name), read(sourcePath), 0644)
	assets, err := filepath.Glob(filepath.Join(out, "*.zip"))
	must(err)
	sort.Strings(assets)
	var checksums strings.Builder
	for _, asset := range assets {
		fmt.Fprintf(&checksums, "%s  %s\n", digest(read(asset)), filepath.Base(asset))
	}
	write(filepath.Join(out, "SHA256SUMS.txt"), []byte(checksums.String()), 0644)
	fmt.Println("Package:", packagePath)
}
