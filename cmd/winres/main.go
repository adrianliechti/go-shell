// Command winres generates the Windows resource objects (rsrc_windows_*.syso)
// for a Go desktop app from a single square source PNG (ideally 1024x1024):
// the app icon, a modern application manifest (per-monitor-v2 DPI awareness,
// common controls v6, long paths) and version info. `go build` links the
// .syso files into the executable; the shell window loads RT_GROUP_ICON #1.
//
// The files are meant to be generated and committed, via a go:generate
// directive next to the main package:
//
//	//go:generate go tool winres -name Bridge -description "Bridge - Kubernetes Dashboard" -company "Adrian Liechti" -in appicon.png
package main

import (
	"flag"
	"fmt"
	"image/png"
	"log"
	"os"

	"github.com/tc-hib/winres"
	"github.com/tc-hib/winres/version"
)

// en-US, the single string table language, matching common conventions.
const langEN = 0x0409

func main() {
	name := flag.String("name", "", "app name (required)")
	description := flag.String("description", "", "file description (default: the app name)")
	company := flag.String("company", "", "company name")
	in := flag.String("in", "appicon.png", "source png")
	out := flag.String("out", "rsrc", "output file prefix")
	flag.Parse()

	if *name == "" {
		flag.Usage()
		os.Exit(2)
	}

	if *description == "" {
		*description = *name
	}

	f, err := os.Open(*in)

	if err != nil {
		log.Fatal(err)
	}

	img, err := png.Decode(f)
	f.Close()

	if err != nil {
		log.Fatal(err)
	}

	icon, err := winres.NewIconFromResizedImage(img, nil)

	if err != nil {
		log.Fatal(err)
	}

	rs := &winres.ResourceSet{}

	// ID 1: the shell's WebView2 window looks up its icon as RT_GROUP_ICON #1.
	if err := rs.SetIcon(winres.ID(1), icon); err != nil {
		log.Fatal(err)
	}

	rs.SetManifest(winres.AppManifest{
		Identity:            winres.AssemblyIdentity{Name: *name, Version: [4]uint16{1, 0, 0, 0}},
		Description:         *description,
		Compatibility:       winres.Win10AndAbove,
		DPIAwareness:        winres.DPIPerMonitorV2,
		LongPathAware:       true,
		UseCommonControlsV6: true,
	})

	// The version numbers are fixed placeholders: stamping real versions into
	// the committed .syso files would dirty the tree on every release.
	vi := version.Info{}
	vi.SetFileVersion("0.0.0.0")
	vi.SetProductVersion("0.0.0.0")

	for _, kv := range []struct{ key, value string }{
		{version.CompanyName, *company},
		{version.FileDescription, *description},
		{version.InternalName, *name},
		{version.OriginalFilename, *name + ".exe"},
		{version.ProductName, *name},
	} {
		if kv.value == "" {
			continue
		}

		if err := vi.Set(langEN, kv.key, kv.value); err != nil {
			log.Fatal(err)
		}
	}

	rs.SetVersionInfo(vi)

	for _, arch := range []winres.Arch{winres.ArchAMD64, winres.ArchARM64} {
		path := fmt.Sprintf("%s_windows_%s.syso", *out, arch)

		obj, err := os.Create(path)

		if err != nil {
			log.Fatal(err)
		}

		if err := rs.WriteObject(obj, arch); err != nil {
			log.Fatal(err)
		}

		if err := obj.Close(); err != nil {
			log.Fatal(err)
		}
	}
}
