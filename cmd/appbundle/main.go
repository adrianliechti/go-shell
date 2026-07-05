// Command appbundle compiles a Go desktop app and assembles a macOS .app
// bundle: Go binary, generated Info.plist, icon.icns rendered from the app's
// source PNG, and an ad-hoc code signature (required on Apple Silicon).
//
// Run from the module root:
//
//	go tool appbundle -name Bridge -id com.example.bridge -package ./app -version 1.2.3
package main

import (
	"flag"
	"fmt"
	"image/png"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/adrianliechti/go-shell/internal/icns"
)

func main() {
	name := flag.String("name", "", "app name (required, names the .app and its binary)")
	id := flag.String("id", "", "bundle identifier (required, e.g. com.example.app)")
	pkg := flag.String("package", ".", "directory of the Go main package to build")
	version := flag.String("version", "0.0.0", "bundle version")
	icon := flag.String("icon", "", "app icon png, ideally 1024x1024 (default <package>/appicon.png)")
	copyright := flag.String("copyright", "", "human-readable copyright")
	out := flag.String("out", "", "output directory (default <package>/build/bin)")
	arch := flag.String("arch", "arm64", "target GOARCH")
	minos := flag.String("minos", "12.0", "macOS deployment target")
	flag.Parse()

	if runtime.GOOS != "darwin" {
		log.Fatal("appbundle assembles a macOS bundle and must run on macOS")
	}

	if *name == "" || *id == "" {
		flag.Usage()
		os.Exit(2)
	}

	if *icon == "" {
		*icon = filepath.Join(*pkg, "appicon.png")
	}

	if *out == "" {
		*out = filepath.Join(*pkg, "build", "bin")
	}

	app := filepath.Join(*out, *name+".app")
	contents := filepath.Join(app, "Contents")

	if err := os.RemoveAll(app); err != nil {
		log.Fatal(err)
	}

	for _, dir := range []string{"MacOS", "Resources"} {
		if err := os.MkdirAll(filepath.Join(contents, dir), 0o755); err != nil {
			log.Fatal(err)
		}
	}

	log.Printf("compiling %s %s", *name, *version)

	// Pin the target OS/arch/deployment target explicitly: left to defaults,
	// the binary's minos silently becomes the build host's OS version, and
	// the arch silently becomes the build host's arch — either can drift from
	// what a release archive name promises.
	//
	// The deployment target goes into CGO_CFLAGS/CGO_LDFLAGS (keeping Go's
	// "-g -O2" defaults) instead of MACOSX_DEPLOYMENT_TARGET: the flags are
	// part of the build-cache key, while the env var is not — cached cgo
	// objects compiled for the host OS would be reused and trigger
	// "object file was built for newer macOS version" link warnings.
	buildEnv := []string{
		"CGO_ENABLED=1",
		"GOOS=darwin",
		"GOARCH=" + *arch,
		"CGO_CFLAGS=-g -O2 -mmacosx-version-min=" + *minos,
		"CGO_LDFLAGS=-g -O2 -mmacosx-version-min=" + *minos,
	}
	runEnv(buildEnv, "go", "build", "-trimpath", "-ldflags=-s -w", "-o", filepath.Join(contents, "MacOS", *name), "./"+filepath.ToSlash(filepath.Clean(*pkg)))

	if err := os.WriteFile(filepath.Join(contents, "Info.plist"), plist(*name, *id, *version, *minos, *copyright), 0o644); err != nil {
		log.Fatal(err)
	}

	if err := renderIcon(*icon, filepath.Join(contents, "Resources", "icon.icns")); err != nil {
		log.Fatal(err)
	}

	run("codesign", "--force", "--sign", "-", app)

	log.Printf("built %s", app)
}

func plist(name, id, version, minos, copyright string) []byte {
	optional := ""

	if copyright != "" {
		optional = fmt.Sprintf(`        <key>NSHumanReadableCopyright</key>
        <string>%s</string>
`, copyright)
	}

	return fmt.Appendf(nil, `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
    <dict>
        <key>CFBundlePackageType</key>
        <string>APPL</string>
        <key>CFBundleName</key>
        <string>%[1]s</string>
        <key>CFBundleDisplayName</key>
        <string>%[1]s</string>
        <key>CFBundleExecutable</key>
        <string>%[1]s</string>
        <key>CFBundleIdentifier</key>
        <string>%[2]s</string>
        <key>CFBundleVersion</key>
        <string>%[3]s</string>
        <key>CFBundleShortVersionString</key>
        <string>%[3]s</string>
        <key>CFBundleIconFile</key>
        <string>icon</string>
        <key>LSMinimumSystemVersion</key>
        <string>%[4]s</string>
        <key>NSHighResolutionCapable</key>
        <true/>
%[5]s        <!-- The app loads its UI from the in-process server on 127.0.0.1. -->
        <key>NSAppTransportSecurity</key>
        <dict>
            <key>NSAllowsLocalNetworking</key>
            <true/>
        </dict>
    </dict>
</plist>
`, name, id, version, minos, optional)
}

func renderIcon(src, dst string) error {
	f, err := os.Open(src)

	if err != nil {
		return err
	}

	img, err := png.Decode(f)
	f.Close()

	if err != nil {
		return err
	}

	out, err := os.Create(dst)

	if err != nil {
		return err
	}

	if err := icns.Encode(out, img); err != nil {
		out.Close()
		return err
	}

	return out.Close()
}

func run(name string, args ...string) {
	runEnv(nil, name, args...)
}

func runEnv(env []string, name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		log.Fatal(err)
	}
}
