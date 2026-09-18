package buildinfo

import "testing"

func TestOnlyABareXYZStampIsARelease(t *testing.T) {
	cases := map[string]bool{
		"0.1.0":     true,
		"v0.1.0":    true, // a tag name, as the releases API reports it
		"10.20.30":  true,
		"0.1.0-dev": false, // the default stamp of a `go build`
		"0.1":       false,
		"0.1.0.1":   false,
		"":          false,
		"latest":    false,
		"0.1.x":     false,
		"0.1.+1":    false,
	}

	for stamp, want := range cases {
		if got := isRelease(stamp); got != want {
			t.Errorf("isRelease(%q) = %v, want %v", stamp, got, want)
		}
	}
}

func TestNewerIsStrictAndNeverSuggestsADowngrade(t *testing.T) {
	cases := []struct {
		candidate, current string
		want               bool
	}{
		{"0.2.0", "0.1.0", true},
		{"0.1.1", "0.1.0", true},
		{"1.0.0", "0.99.99", true},
		{"v0.2.0", "0.1.0", true}, // the tag form still orders
		{"0.1.0", "0.1.0", false}, // same version is not newer
		{"0.1.0", "0.2.0", false}, // a downgrade is never offered
		{"0.9.0", "0.10.0", false},
		{"0.2.0", "0.1.0-dev", false}, // a dev build has nothing to compare against
		{"garbage", "0.1.0", false},
	}

	for _, c := range cases {
		if got := Newer(c.candidate, c.current); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.candidate, c.current, got, c.want)
		}
	}
}

func TestTheDefaultStampIsADevelopmentBuild(t *testing.T) {
	// The un-stamped build must never look like a release: it is what gates the
	// update check and `dbn update` off for anyone running their own build.
	if IsRelease() {
		t.Errorf("the default build stamp %q reports itself as a release", Version())
	}
}
