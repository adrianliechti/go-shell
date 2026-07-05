// Package winres generates the Windows resource object (rsrc_windows_*.syso)
// for a Go desktop app from a single square source image: the app icon, a
// modern application manifest (per-monitor-v2 DPI awareness, common controls
// v6, long paths) and version info. `go build` links a .syso placed in the
// main package's directory into the executable; the shell window loads its
// icon as RT_GROUP_ICON #1.
package winres

import (
	"image"
	"os"

	"github.com/tc-hib/winres"
	"github.com/tc-hib/winres/version"
)

// en-US, the single string table language, matching common conventions.
const langEN = 0x0409

type Options struct {
	Name        string
	Description string
	Company     string

	Icon image.Image
}

// Syso writes the resource object for arch ("amd64" or "arm64") to path.
func Syso(path string, arch string, opts Options) error {
	if opts.Description == "" {
		opts.Description = opts.Name
	}

	icon, err := winres.NewIconFromResizedImage(opts.Icon, nil)

	if err != nil {
		return err
	}

	rs := &winres.ResourceSet{}

	// ID 1: the shell's WebView2 window looks up its icon as RT_GROUP_ICON #1.
	if err := rs.SetIcon(winres.ID(1), icon); err != nil {
		return err
	}

	rs.SetManifest(winres.AppManifest{
		Identity:            winres.AssemblyIdentity{Name: opts.Name, Version: [4]uint16{1, 0, 0, 0}},
		Description:         opts.Description,
		Compatibility:       winres.Win10AndAbove,
		DPIAwareness:        winres.DPIPerMonitorV2,
		LongPathAware:       true,
		UseCommonControlsV6: true,
	})

	// The version numbers are fixed placeholders; the meaningful release
	// version lives in the release artifacts, not the file metadata.
	vi := version.Info{}
	vi.SetFileVersion("0.0.0.0")
	vi.SetProductVersion("0.0.0.0")

	for _, kv := range []struct{ key, value string }{
		{version.CompanyName, opts.Company},
		{version.FileDescription, opts.Description},
		{version.InternalName, opts.Name},
		{version.OriginalFilename, opts.Name + ".exe"},
		{version.ProductName, opts.Name},
	} {
		if kv.value == "" {
			continue
		}

		if err := vi.Set(langEN, kv.key, kv.value); err != nil {
			return err
		}
	}

	rs.SetVersionInfo(vi)

	f, err := os.Create(path)

	if err != nil {
		return err
	}

	if err := rs.WriteObject(f, winres.Arch(arch)); err != nil {
		f.Close()
		return err
	}

	return f.Close()
}
