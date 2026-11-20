package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestExperimentalFeaturesStartOff(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"version": 4, "connections": {}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range Features {
		if c.Experimental.On(f) {
			t.Errorf("%s is on in a config that never set it", f)
		}
		if err := c.Experimental.Check(f); !errors.Is(err, ErrFeatureOff) {
			t.Errorf("Check(%s) = %v, want ErrFeatureOff", f, err)
		}
	}
	want := "Provision is experimental and off. Turn it on with: sshctl experimental enable provision"
	if err := c.Experimental.Check(FeatureProvision); err.Error() != want {
		t.Errorf("Check(provision) = %q, want %q", err, want)
	}
}

func TestExperimentalFeaturesAreSavedInConfigJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	c := New()
	if err := c.Experimental.Set(FeatureBootstrap, true); err != nil {
		t.Fatal(err)
	}
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	keys := fileKeys(t, path)
	found := false
	for _, k := range keys {
		found = found || k == "experimental"
	}
	if !found {
		t.Errorf("config.json keys = %v, want experimental", keys)
	}
	got, err := Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Experimental.On(FeatureBootstrap) || got.Experimental.On(FeatureProvision) {
		t.Errorf("loaded %+v, want only bootstrap on", got.Experimental)
	}
	if err := got.Experimental.Check(FeatureBootstrap); err != nil {
		t.Errorf("Check(bootstrap) = %v, want nil", err)
	}
}

func TestSetRefusesAnUnknownFeature(t *testing.T) {
	var e Experimental
	if err := e.Set("sync", true); !errors.Is(err, ErrUnknownFeature) {
		t.Fatalf("Set(sync) = %v, want ErrUnknownFeature", err)
	}
	if e != (Experimental{}) {
		t.Errorf("Set(sync) changed %+v", e)
	}
}
