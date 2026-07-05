// Command appbundle compiles a Go desktop app and assembles a macOS .app
// bundle: Go binary, Info.plist with the version stamped in (every
// __VERSION__ placeholder is replaced), the app icon, and an ad-hoc code
// signature (required on Apple Silicon).
//
// The Info.plist template and icon.icns live next to the main package and
// are picked up from there by default. Run from the module root:
//
//	go tool appbundle -name Bridge -package ./app -version 1.2.3
package main

import (
	"bytes"
	"flag"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

func main() {
	name := flag.String("name", "", "app name (required, names the .app and its binary)")
	pkg := flag.String("package", ".", "directory of the Go main package to build")
	version := flag.String("version", "0.0.0", "bundle version")
	icon := flag.String("icon", "", "app icon (default <package>/icon.icns)")
	plist := flag.String("plist", "", "Info.plist template (default <package>/Info.plist)")
	out := flag.String("out", "", "output directory (default <package>/build/bin)")
	arch := flag.String("arch", "arm64", "target GOARCH")
	minos := flag.String("minos", "12.0", "macOS deployment target; keep in sync with LSMinimumSystemVersion in the Info.plist")
	flag.Parse()

	if runtime.GOOS != "darwin" {
		log.Fatal("appbundle assembles a macOS bundle and must run on macOS")
	}

	if *name == "" {
		flag.Usage()
		os.Exit(2)
	}

	if *icon == "" {
		*icon = filepath.Join(*pkg, "icon.icns")
	}

	if *plist == "" {
		*plist = filepath.Join(*pkg, "Info.plist")
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

	data, err := os.ReadFile(*plist)

	if err != nil {
		log.Fatal(err)
	}

	data = bytes.ReplaceAll(data, []byte("__VERSION__"), []byte(*version))

	if err := os.WriteFile(filepath.Join(contents, "Info.plist"), data, 0o644); err != nil {
		log.Fatal(err)
	}

	icns, err := os.ReadFile(*icon)

	if err != nil {
		log.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(contents, "Resources", "icon.icns"), icns, 0o644); err != nil {
		log.Fatal(err)
	}

	run("codesign", "--force", "--sign", "-", app)

	log.Printf("built %s", app)
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
