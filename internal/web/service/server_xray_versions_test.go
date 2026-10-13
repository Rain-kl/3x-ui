package service

import (
	"reflect"
	"testing"
)

func TestFeatXrayReleaseVersions(t *testing.T) {
	releases := []Release{
		{TagName: "v26.10.10-feat.1"},
		{TagName: "v26.10.10-feat.12"},
		{TagName: "v26.9.99-feat.99"},
		{TagName: "v27.0.0-feat.1"},
		{TagName: "v28.0.0-feat.1", Draft: true},
		{TagName: "v29.0.0-feat.1", Prerelease: true},
		{TagName: "v30.0.0-audit.1"},
		{TagName: "not-a-version"},
	}
	want := []string{
		"v27.0.0-feat.1",
		"v26.10.10-feat.12",
		"v26.10.10-feat.1",
		"v26.9.99-feat.99",
	}
	if got := featXrayReleaseVersions(releases); !reflect.DeepEqual(got, want) {
		t.Fatalf("featXrayReleaseVersions() = %v, want %v", got, want)
	}
}
