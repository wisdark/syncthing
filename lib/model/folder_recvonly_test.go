// Copyright (C) 2018 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

package model

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/syncthing/syncthing/lib/config"
	"github.com/syncthing/syncthing/lib/db"
	"github.com/syncthing/syncthing/lib/db/backend"
	"github.com/syncthing/syncthing/lib/fs"
	"github.com/syncthing/syncthing/lib/protocol"
	"github.com/syncthing/syncthing/lib/scanner"
)

func TestRecvOnlyRevertDeletes(t *testing.T) {
	// Make sure that we delete extraneous files and directories when we hit
	// Revert.

	// Get us a model up and running

	m, f := setupROFolder(t)
	ffs := f.Filesystem()
	defer cleanupModel(m)

	// Create some test data

	for _, dir := range []string{".stfolder", "ignDir", "unknownDir"} {
		must(t, ffs.MkdirAll(dir, 0755))
	}
	must(t, writeFile(ffs, "ignDir/ignFile", []byte("hello\n"), 0644))
	must(t, writeFile(ffs, "unknownDir/unknownFile", []byte("hello\n"), 0644))
	must(t, writeFile(ffs, ".stignore", []byte("ignDir\n"), 0644))

	knownFiles := setupKnownFiles(t, ffs, []byte("hello\n"))

	// Send and index update for the known stuff

	m.Index(device1, "ro", knownFiles)
	f.updateLocalsFromScanning(knownFiles)

	m.fmut.RLock()
	snap := m.folderFiles["ro"].Snapshot()
	m.fmut.RUnlock()
	size := snap.GlobalSize()
	snap.Release()
	if size.Files != 1 || size.Directories != 1 {
		t.Fatalf("Global: expected 1 file and 1 directory: %+v", size)
	}

	// Scan, should discover the other stuff in the folder

	must(t, m.ScanFolder("ro"))

	// We should now have two files and two directories.

	size = globalSize(t, m, "ro")
	if size.Files != 2 || size.Directories != 2 {
		t.Fatalf("Global: expected 2 files and 2 directories: %+v", size)
	}
	size = localSize(t, m, "ro")
	if size.Files != 2 || size.Directories != 2 {
		t.Fatalf("Local: expected 2 files and 2 directories: %+v", size)
	}
	size = receiveOnlyChangedSize(t, m, "ro")
	if size.Files+size.Directories == 0 {
		t.Fatalf("ROChanged: expected something: %+v", size)
	}

	// Revert should delete the unknown stuff

	m.Revert("ro")

	// These should still exist
	for _, p := range []string{"knownDir/knownFile", "ignDir/ignFile"} {
		if _, err := ffs.Stat(p); err != nil {
			t.Error("Unexpected error:", err)
		}
	}

	// These should have been removed
	for _, p := range []string{"unknownDir", "unknownDir/unknownFile"} {
		if _, err := ffs.Stat(p); !fs.IsNotExist(err) {
			t.Error("Unexpected existing thing:", p)
		}
	}

	// We should now have one file and directory again.

	size = globalSize(t, m, "ro")
	if size.Files != 1 || size.Directories != 1 {
		t.Fatalf("Global: expected 1 files and 1 directories: %+v", size)
	}
	size = localSize(t, m, "ro")
	if size.Files != 1 || size.Directories != 1 {
		t.Fatalf("Local: expected 1 files and 1 directories: %+v", size)
	}
}

func TestRecvOnlyRevertNeeds(t *testing.T) {
	// Make sure that a new file gets picked up and considered latest, then
	// gets considered old when we hit Revert.

	// Get us a model up and running

	m, f := setupROFolder(t)
	ffs := f.Filesystem()
	defer cleanupModel(m)

	// Create some test data

	must(t, ffs.MkdirAll(".stfolder", 0755))
	oldData := []byte("hello\n")
	knownFiles := setupKnownFiles(t, ffs, oldData)

	// Send and index update for the known stuff

	m.Index(device1, "ro", knownFiles)
	f.updateLocalsFromScanning(knownFiles)

	// Scan the folder.

	must(t, m.ScanFolder("ro"))

	// Everything should be in sync.

	size := globalSize(t, m, "ro")
	if size.Files != 1 || size.Directories != 1 {
		t.Fatalf("Global: expected 1 file and 1 directory: %+v", size)
	}
	size = localSize(t, m, "ro")
	if size.Files != 1 || size.Directories != 1 {
		t.Fatalf("Local: expected 1 file and 1 directory: %+v", size)
	}
	size = needSize(t, m, "ro")
	if size.Files+size.Directories > 0 {
		t.Fatalf("Need: expected nothing: %+v", size)
	}
	size = receiveOnlyChangedSize(t, m, "ro")
	if size.Files+size.Directories > 0 {
		t.Fatalf("ROChanged: expected nothing: %+v", size)
	}

	// Update the file.

	newData := []byte("totally different data\n")
	must(t, writeFile(ffs, "knownDir/knownFile", newData, 0644))

	// Rescan.

	must(t, m.ScanFolder("ro"))

	// We now have a newer file than the rest of the cluster. Global state should reflect this.

	size = globalSize(t, m, "ro")
	const sizeOfDir = 128
	if size.Files != 1 || size.Bytes != sizeOfDir+int64(len(oldData)) {
		t.Fatalf("Global: expected no change due to the new file: %+v", size)
	}
	size = localSize(t, m, "ro")
	if size.Files != 1 || size.Bytes != sizeOfDir+int64(len(newData)) {
		t.Fatalf("Local: expected the new file to be reflected: %+v", size)
	}
	size = needSize(t, m, "ro")
	if size.Files+size.Directories > 0 {
		t.Fatalf("Need: expected nothing: %+v", size)
	}
	size = receiveOnlyChangedSize(t, m, "ro")
	if size.Files+size.Directories == 0 {
		t.Fatalf("ROChanged: expected something: %+v", size)
	}

	// We hit the Revert button. The file that was new should become old.

	m.Revert("ro")

	size = globalSize(t, m, "ro")
	if size.Files != 1 || size.Bytes != sizeOfDir+int64(len(oldData)) {
		t.Fatalf("Global: expected the global size to revert: %+v", size)
	}
	size = localSize(t, m, "ro")
	if size.Files != 1 || size.Bytes != sizeOfDir+int64(len(newData)) {
		t.Fatalf("Local: expected the local size to remain: %+v", size)
	}
	size = needSize(t, m, "ro")
	if size.Files != 1 || size.Bytes != int64(len(oldData)) {
		t.Fatalf("Local: expected to need the old file data: %+v", size)
	}
}

func TestRecvOnlyUndoChanges(t *testing.T) {
	// Get us a model up and running

	m, f := setupROFolder(t)
	ffs := f.Filesystem()
	defer cleanupModel(m)

	// Create some test data

	must(t, ffs.MkdirAll(".stfolder", 0755))
	oldData := []byte("hello\n")
	knownFiles := setupKnownFiles(t, ffs, oldData)

	// Send an index update for the known stuff

	m.Index(device1, "ro", knownFiles)
	f.updateLocalsFromScanning(knownFiles)

	// Scan the folder.

	must(t, m.ScanFolder("ro"))

	// Everything should be in sync.

	size := globalSize(t, m, "ro")
	if size.Files != 1 || size.Directories != 1 {
		t.Fatalf("Global: expected 1 file and 1 directory: %+v", size)
	}
	size = localSize(t, m, "ro")
	if size.Files != 1 || size.Directories != 1 {
		t.Fatalf("Local: expected 1 file and 1 directory: %+v", size)
	}
	size = needSize(t, m, "ro")
	if size.Files+size.Directories > 0 {
		t.Fatalf("Need: expected nothing: %+v", size)
	}
	size = receiveOnlyChangedSize(t, m, "ro")
	if size.Files+size.Directories > 0 {
		t.Fatalf("ROChanged: expected nothing: %+v", size)
	}

	// Create a file and modify another

	const file = "foo"
	must(t, writeFile(ffs, file, []byte("hello\n"), 0644))
	must(t, writeFile(ffs, "knownDir/knownFile", []byte("bye\n"), 0644))

	must(t, m.ScanFolder("ro"))

	size = receiveOnlyChangedSize(t, m, "ro")
	if size.Files != 2 {
		t.Fatalf("Receive only: expected 2 files: %+v", size)
	}

	// Remove the file again and undo the modification

	must(t, ffs.Remove(file))
	must(t, writeFile(ffs, "knownDir/knownFile", oldData, 0644))
	must(t, ffs.Chtimes("knownDir/knownFile", knownFiles[1].ModTime(), knownFiles[1].ModTime()))

	must(t, m.ScanFolder("ro"))

	size = receiveOnlyChangedSize(t, m, "ro")
	if size.Files+size.Directories+size.Deleted != 0 {
		t.Fatalf("Receive only: expected all zero: %+v", size)
	}
}

func setupKnownFiles(t *testing.T, ffs fs.Filesystem, data []byte) []protocol.FileInfo {
	t.Helper()

	must(t, ffs.MkdirAll("knownDir", 0755))
	must(t, writeFile(ffs, "knownDir/knownFile", data, 0644))

	t0 := time.Now().Add(-1 * time.Minute)
	must(t, ffs.Chtimes("knownDir/knownFile", t0, t0))

	fi, err := ffs.Stat("knownDir/knownFile")
	if err != nil {
		t.Fatal(err)
	}
	blocks, _ := scanner.Blocks(context.TODO(), bytes.NewReader(data), protocol.BlockSize(int64(len(data))), int64(len(data)), nil, true)
	knownFiles := []protocol.FileInfo{
		{
			Name:        "knownDir",
			Type:        protocol.FileInfoTypeDirectory,
			Permissions: 0755,
			Version:     protocol.Vector{Counters: []protocol.Counter{{ID: 42, Value: 42}}},
			Sequence:    42,
		},
		{
			Name:        "knownDir/knownFile",
			Type:        protocol.FileInfoTypeFile,
			Permissions: 0644,
			Size:        fi.Size(),
			ModifiedS:   fi.ModTime().Unix(),
			ModifiedNs:  int32(fi.ModTime().UnixNano() % 1e9),
			Version:     protocol.Vector{Counters: []protocol.Counter{{ID: 42, Value: 42}}},
			Sequence:    42,
			Blocks:      blocks,
		},
	}

	return knownFiles
}

func setupROFolder(t *testing.T) (*model, *receiveOnlyFolder) {
	t.Helper()

	w := createTmpWrapper(defaultCfg)
	fcfg := testFolderConfigFake()
	fcfg.ID = "ro"
	fcfg.Label = "ro"
	fcfg.Type = config.FolderTypeReceiveOnly
	w.SetFolder(fcfg)

	m := newModel(w, myID, "syncthing", "dev", db.NewLowlevel(backend.OpenMemory()), nil)
	m.ServeBackground()
	must(t, m.ScanFolder("ro"))

	m.fmut.RLock()
	defer m.fmut.RUnlock()
	f := m.folderRunners["ro"].(*receiveOnlyFolder)

	return m, f
}

func writeFile(fs fs.Filesystem, filename string, data []byte, perm fs.FileMode) error {
	fd, err := fs.Create(filename)
	if err != nil {
		return err
	}
	_, err = fd.Write(data)
	if err != nil {
		return err
	}
	if err := fd.Close(); err != nil {
		return err
	}
	return fs.Chmod(filename, perm)
}
