// Copyright (C) 2016 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

package fs

import (
	"os"
	"time"

	"github.com/syncthing/syncthing/lib/osutil"
)

// The database is where we store the virtual mtimes
type database interface {
	Bytes(key string) (data []byte, ok bool)
	PutBytes(key string, data []byte)
	Delete(key string)
}

// variable so that we can mock it for testing
var osChtimes = os.Chtimes

// The MtimeFS is a filesystem with nanosecond mtime precision, regardless
// of what shenanigans the underlying filesystem gets up to. A nil MtimeFS
// just does the underlying operations with no additions.
type MtimeFS struct {
	db database
}

func NewMtimeFS(db database) *MtimeFS {
	return &MtimeFS{
		db: db,
	}
}

func (f *MtimeFS) Chtimes(name string, atime, mtime time.Time) error {
	if f == nil {
		return osChtimes(name, atime, mtime)
	}

	// Do a normal Chtimes call, don't care if it succeeds or not.
	osChtimes(name, atime, mtime)

	// Stat the file to see what happened. Here we *do* return an error,
	// because it might be "does not exist" or similar. osutil.Lstat is the
	// souped up version to account for Android breakage.
	info, err := osutil.Lstat(name)
	if err != nil {
		return err
	}

	f.save(name, info.ModTime(), mtime)
	return nil
}

func (f *MtimeFS) Lstat(name string) (os.FileInfo, error) {
	if f == nil {
		return osutil.Lstat(name)
	}

	info, err := osutil.Lstat(name)
	if err != nil {
		return nil, err
	}

	real, virtual := f.load(name)
	if real == info.ModTime() {
		info = mtimeFileInfo{
			FileInfo: info,
			mtime:    virtual,
		}
	}

	return info, nil
}

// "real" is the on disk timestamp
// "virtual" is what want the timestamp to be

func (f *MtimeFS) save(name string, real, virtual time.Time) {
	if real.Equal(virtual) {
		// If the virtual time and the real on disk time are equal we don't
		// need to store anything.
		f.db.Delete(name)
		return
	}

	mtime := dbMtime{
		real:    real,
		virtual: virtual,
	}
	bs, _ := mtime.Marshal() // Can't fail
	f.db.PutBytes(name, bs)
}

func (f *MtimeFS) load(name string) (real, virtual time.Time) {
	data, exists := f.db.Bytes(name)
	if !exists {
		return
	}

	var mtime dbMtime
	if err := mtime.Unmarshal(data); err != nil {
		return
	}

	return mtime.real, mtime.virtual
}

// The mtimeFileInfo is an os.FileInfo that lies about the ModTime().

type mtimeFileInfo struct {
	os.FileInfo
	mtime time.Time
}

func (m mtimeFileInfo) ModTime() time.Time {
	return m.mtime
}

// The dbMtime is our database representation

type dbMtime struct {
	real    time.Time
	virtual time.Time
}

func (t *dbMtime) Marshal() ([]byte, error) {
	bs0, _ := t.real.MarshalBinary()
	bs1, _ := t.virtual.MarshalBinary()
	return append(bs0, bs1...), nil
}

func (t *dbMtime) Unmarshal(bs []byte) error {
	if err := t.real.UnmarshalBinary(bs[:len(bs)/2]); err != nil {
		return err
	}
	if err := t.virtual.UnmarshalBinary(bs[len(bs)/2:]); err != nil {
		return err
	}
	return nil
}
