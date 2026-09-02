package model_test

import (
	"encoding/base32"
	"strings"
	"testing"

	"github.com/tedkulp/drops/internal/model"
)

func TestGeneratedKeysAreCanonical128BitBase32(t *testing.T) {
	t.Parallel()

	projectKey, err := model.NewProjectKey()
	if err != nil {
		t.Fatalf("NewProjectKey: %v", err)
	}
	replicaKey, err := model.NewReplicaKey()
	if err != nil {
		t.Fatalf("NewReplicaKey: %v", err)
	}

	for name, value := range map[string]string{
		"project": string(projectKey),
		"replica": string(replicaKey),
	} {
		t.Run(name, func(t *testing.T) {
			if len(value) != 26 {
				t.Fatalf("key length = %d, want 26: %q", len(value), value)
			}
			decoded, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(value))
			if err != nil {
				t.Fatalf("key is not unpadded Base32: %q: %v", value, err)
			}
			if len(decoded) != 16 {
				t.Fatalf("decoded key length = %d, want 16", len(decoded))
			}
			if value != strings.ToLower(value) {
				t.Errorf("key = %q, want canonical lowercase", value)
			}
		})
	}
}

func TestReservedProjectsHavePermanentKeys(t *testing.T) {
	t.Parallel()

	if got, want := model.GlobalProjectKey, model.ProjectKey("jvxtjayrcs7vdguuqche7njh2y"); got != want {
		t.Errorf("GlobalProjectKey = %q, want %q", got, want)
	}
	if got, want := model.InboxProjectKey, model.ProjectKey("h2sbybyal4ryua23cbfb5wvawq"); got != want {
		t.Errorf("InboxProjectKey = %q, want %q", got, want)
	}
	if model.GlobalProjectKey == model.InboxProjectKey {
		t.Error("reserved Projects share a key")
	}
}

func TestKeyParsingRejectsNoncanonicalText(t *testing.T) {
	t.Parallel()

	valid := "jvxtjayrcs7vdguuqche7njh2y"
	for name, raw := range map[string]string{
		"empty":        "",
		"prefixed":     "replica-" + valid,
		"uppercase":    strings.ToUpper(valid),
		"short":        valid[:25],
		"bad alphabet": valid[:25] + "0",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := model.ParseProjectKey(raw); err == nil {
				t.Errorf("ParseProjectKey(%q) succeeded", raw)
			}
			if _, err := model.ParseReplicaKey(raw); err == nil {
				t.Errorf("ParseReplicaKey(%q) succeeded", raw)
			}
		})
	}

	if got, err := model.ParseProjectKey(valid); err != nil || string(got) != valid {
		t.Fatalf("ParseProjectKey(valid) = %q, %v", got, err)
	}
	if got, err := model.ParseReplicaKey(valid); err != nil || string(got) != valid {
		t.Fatalf("ParseReplicaKey(valid) = %q, %v", got, err)
	}
}

func TestEntityIDsAreOpaqueAndPreserved(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"k3f9x", "br-6vf", "beacon-ci-never-executed-4bhg", "mem-old-shape"} {
		got, err := model.ParseID(raw)
		if err != nil {
			t.Errorf("ParseID(%q): %v", raw, err)
			continue
		}
		if string(got) != raw {
			t.Errorf("ParseID(%q) = %q", raw, got)
		}
	}
	if _, err := model.ParseID(""); err == nil {
		t.Error("ParseID(empty) succeeded")
	}
}
