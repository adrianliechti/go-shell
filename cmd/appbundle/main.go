// Command appbundle compiles a Go desktop app and packages it for the target
// platform, generating all platform resources from the app's single source
// PNG (ideally 1024x1024):
//
//   - macOS: assembles <name>.app — Go binary, generated Info.plist,
//     icon.icns rendered from the PNG, and an ad-hoc code signature
//     (required on Apple Silicon). Must run on macOS.
//   - Windows: builds <name>.exe (windowsgui) — a resource object (icon,
//     per-monitor-v2 DPI manifest, version info) is generated next to the
//     main package for the duration of the build and removed afterwards.
//
// Run from the module root:
//
//	go tool appbundle -name Bridge -id com.example.bridge -package ./app -version 1.2.3
package main

import (
	"flag"
	"fmt"
	"image"
	"image/png"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/adrianliechti/go-shell/internal/icns"
	"github.com/adrianliechti/go-shell/internal/winres"
)

func main() {
	name := flag.String("name", "", "app name (required, names the .app / .exe)")
	id := flag.String("id", "", "bundle identifier, e.g. com.example.app (required on macOS)")
	pkg := flag.String("package", ".", "directory of the Go main package to build")
	version := flag.String("version", "0.0.0", "bundle version")
	description := flag.String("description", "", "file description shown by Windows (default: the app name)")
	company := flag.String("company", "", "company name shown by Windows")
	copyright := flag.String("copyright", "", "human-readable copyright shown by macOS")
	icon := flag.String("icon", "", "app icon png, ideally 1024x1024 (default <package>/appicon.png)")
	out := flag.String("out", "", "output directory (default <package>/build/bin)")
	goos := flag.String("os", runtime.GOOS, "target platform: darwin or windows")
	arch := flag.String("arch", "", "target GOARCH (default: arm64 on darwin, the host arch on windows)")
	minos := flag.String("minos", "12.0", "macOS deployment target")
	flag.Parse()

	if *name == "" {
		flag.Usage()
		os.Exit(2)
	}

	if *icon == "" {
		*icon = filepath.Join(*pkg, "appicon.png")
	}

	if *out == "" {
		*out = filepath.Join(*pkg, "build", "bin")
	}

	img := loadIcon(*icon)

	switch *goos {
	case "darwin":
		if runtime.GOOS != "darwin" {
			log.Fatal("the macOS bundle must be assembled on macOS (cgo/Cocoa, codesign)")
		}

		if *id == "" {
			flag.Usage()
			os.Exit(2)
		}

		if *arch == "" {
			*arch = "arm64"
		}

		bundleDarwin(*name, *id, *pkg, *version, *copyright, *out, *arch, *minos, img)

	case "windows":
		if *arch == "" {
			*arch = hostWindowsArch()
		}

		buildWindows(*name, *pkg, *description, *company, *out, *arch, img)

	default:
		log.Fatalf("unsupported target platform %q", *goos)
	}
}

func bundleDarwin(name, id, pkg, version, copyright, out, arch, minos string, icon image.Image) {
	app := filepath.Join(out, name+".app")
	contents := filepath.Join(app, "Contents")

	if err := os.RemoveAll(app); err != nil {
		log.Fatal(err)
	}

	for _, dir := range []string{"MacOS", "Resources"} {
		if err := os.MkdirAll(filepath.Join(contents, dir), 0o755); err != nil {
			log.Fatal(err)
		}
	}

	log.Printf("compiling %s %s", name, version)

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
		"GOARCH=" + arch,
		"CGO_CFLAGS=-g -O2 -mmacosx-version-min=" + minos,
		"CGO_LDFLAGS=-g -O2 -mmacosx-version-min=" + minos,
	}
	runEnv(buildEnv, "go", "build", "-trimpath", "-ldflags=-s -w", "-o", filepath.Join(contents, "MacOS", name), pkgPath(pkg))

	if err := os.WriteFile(filepath.Join(contents, "Info.plist"), plist(name, id, version, minos, copyright), 0o644); err != nil {
		log.Fatal(err)
	}

	if err := writeIcns(filepath.Join(contents, "Resources", "icon.icns"), icon); err != nil {
		log.Fatal(err)
	}

	run("codesign", "--force", "--sign", "-", app)

	log.Printf("built %s", app)
}

func buildWindows(name, pkg, description, company, out, arch string, icon image.Image) {
	// The linker only picks up resource objects that sit in a package
	// directory, so the .syso is generated next to the main package for the
	// duration of the build.
	syso := filepath.Join(pkg, "rsrc_windows_"+arch+".syso")

	err := winres.Syso(syso, arch, winres.Options{
		Name:        name,
		Description: description,
		Company:     company,

		Icon: icon,
	})

	if err != nil {
		log.Fatal(err)
	}

	exe := filepath.Join(out, name+".exe")

	log.Printf("compiling %s", name)

	buildEnv := []string{
		"GOOS=windows",
		"GOARCH=" + arch,
	}

	// Remove the .syso even when the build fails (log.Fatal skips defers):
	// a stray leftover would silently be linked into plain `go build` runs.
	buildErr := runEnvErr(buildEnv, "go", "build", "-trimpath", "-ldflags=-s -w -H windowsgui", "-o", exe, pkgPath(pkg))
	os.Remove(syso)

	if buildErr != nil {
		log.Fatal(buildErr)
	}

	log.Printf("built %s", exe)
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

func loadIcon(path string) image.Image {
	f, err := os.Open(path)

	if err != nil {
		log.Fatal(err)
	}

	defer f.Close()

	img, err := png.Decode(f)

	if err != nil {
		log.Fatal(err)
	}

	return img
}

func writeIcns(path string, img image.Image) error {
	f, err := os.Create(path)

	if err != nil {
		return err
	}

	if err := icns.Encode(f, img); err != nil {
		f.Close()
		return err
	}

	return f.Close()
}

func hostWindowsArch() string {
	if runtime.GOOS == "windows" {
		return runtime.GOARCH
	}

	return "amd64"
}

func pkgPath(pkg string) string {
	return "./" + filepath.ToSlash(filepath.Clean(pkg))
}

func run(name string, args ...string) {
	runEnv(nil, name, args...)
}

func runEnv(env []string, name string, args ...string) {
	if err := runEnvErr(env, name, args...); err != nil {
		log.Fatal(err)
	}
}

func runEnvErr(env []string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}
