package sync

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tedkulp/drops/internal/model"
)

func TestLoadSidecarAbsentIsNil(t *testing.T) {
	sidecar, err := LoadSidecar(t.TempDir())
	if err != nil {
		t.Fatalf("LoadSidecar: %v", err)
	}
	if sidecar != nil {
		t.Fatalf("LoadSidecar = %#v, want nil for an absent sidecar", sidecar)
	}
}

func TestMintThenLoadRoundTrips(t *testing.T) {
	dir := t.TempDir()
	minted, err := MintSidecar(dir)
	if err != nil {
		t.Fatalf("MintSidecar: %v", err)
	}
	if minted.LastExportHead != "" {
		t.Fatalf("minted last export head = %q, want empty", minted.LastExportHead)
	}
	loaded, err := LoadSidecar(dir)
	if err != nil {
		t.Fatalf("LoadSidecar: %v", err)
	}
	if loaded == nil {
		t.Fatal("LoadSidecar = nil, want the minted sidecar")
	}
	if loaded.Replica != minted.Replica {
		t.Fatalf("loaded replica = %q, want %q", loaded.Replica, minted.Replica)
	}
	fi, err := os.Stat(filepath.Join(dir, sidecarFileName))
	if err != nil {
		t.Fatalf("stat sidecar: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != fileMode {
		t.Fatalf("sidecar mode = %04o, want %04o", perm, fileMode)
	}
}

func TestSavePersistsExportHead(t *testing.T) {
	dir := t.TempDir()
	sidecar := Sidecar{Replica: model.ReplicaKey("aaaaaaaaaaaaaaaaaaaaaaaaae"), LastExportHead: "0123456789abcdef"}
	if err := sidecar.Save(dir); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := LoadSidecar(dir)
	if err != nil {
		t.Fatalf("LoadSidecar: %v", err)
	}
	if loaded.LastExportHead != "0123456789abcdef" {
		t.Fatalf("loaded last export head = %q, want the saved head", loaded.LastExportHead)
	}
}

func TestLoadMalformedSidecarIsHardError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, sidecarFileName)
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write malformed sidecar: %v", err)
	}
	if _, err := LoadSidecar(dir); err == nil {
		t.Fatal("LoadSidecar on malformed sidecar = nil error, want hard error")
	}
}

func TestLoadInvalidReplicaKeyIsHardError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, sidecarFileName)
	body := []byte(`{"replica_key":"not-a-key","last_export_head":""}` + "\n")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("write invalid sidecar: %v", err)
	}
	if _, err := LoadSidecar(dir); err == nil {
		t.Fatal("LoadSidecar on invalid replica key = nil error, want hard error")
	}
}

func TestLoadUnknownMemberIsHardError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, sidecarFileName)
	body := []byte(`{"replica_key":"aaaaaaaaaaaaaaaaaaaaaaaaae","surprise":1}` + "\n")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("write sidecar with unknown member: %v", err)
	}
	if _, err := LoadSidecar(dir); err == nil {
		t.Fatal("LoadSidecar on unknown member = nil error, want hard error")
	}
}

func TestSaveInvalidReplicaKeyIsRejected(t *testing.T) {
	sidecar := Sidecar{Replica: model.ReplicaKey("short"), LastExportHead: ""}
	if err := sidecar.Save(t.TempDir()); err == nil {
		t.Fatal("Save with invalid replica key = nil error, want rejection")
	}
}
