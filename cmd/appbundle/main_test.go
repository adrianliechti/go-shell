package main

import (
	"encoding/xml"
	"testing"
)

func TestPlistMetadataRoundTrip(t *testing.T) {
	for _, copyright := range []string{"", `© A & B <company> "2026"`} {
		t.Run(copyright, func(t *testing.T) {
			name := `Research & Notes <desktop> "edition"`
			id, version, minos := "com.example.a&b", "1.0<2", "12.0&later"
			var decoded struct {
				Dict struct {
					Elements []struct {
						XMLName xml.Name
						Text    string `xml:",chardata"`
					} `xml:",any"`
				} `xml:"dict"`
			}
			if err := xml.Unmarshal(plist(name, id, version, minos, copyright), &decoded); err != nil {
				t.Fatalf("invalid plist XML: %v", err)
			}
			values := make(map[string]string)
			entries := decoded.Dict.Elements
			if len(entries)%2 != 0 {
				t.Fatal("plist dictionary has an unmatched key/value")
			}
			for i := 0; i < len(entries); i += 2 {
				if entries[i].XMLName.Local != "key" {
					t.Fatalf("expected key, got %s", entries[i].XMLName.Local)
				}
				if entries[i+1].XMLName.Local == "string" {
					values[entries[i].Text] = entries[i+1].Text
				}
			}
			for key, want := range map[string]string{
				"CFBundleName": name, "CFBundleDisplayName": name,
				"CFBundleExecutable": name, "CFBundleIdentifier": id,
				"CFBundleVersion": version, "CFBundleShortVersionString": version,
				"LSMinimumSystemVersion": minos,
			} {
				if got := values[key]; got != want {
					t.Errorf("%s = %q, want %q", key, got, want)
				}
			}
			got, present := values["NSHumanReadableCopyright"]
			if got != copyright || present != (copyright != "") {
				t.Errorf("copyright = %q (present %t), want %q", got, present, copyright)
			}
		})
	}
}
