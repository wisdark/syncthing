// Copyright (C) 2015 The Syncthing Authors.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at https://mozilla.org/MPL/2.0/.

package main

import (
	"encoding/binary"
	"fmt"
	"log"
	"time"

	"github.com/syncthing/syncthing/lib/db"
	"github.com/syncthing/syncthing/lib/db/backend"
	"github.com/syncthing/syncthing/lib/protocol"
)

func dump(ldb backend.Backend) {
	it, err := ldb.NewPrefixIterator(nil)
	if err != nil {
		log.Fatal(err)
	}
	for it.Next() {
		key := it.Key()
		switch key[0] {
		case db.KeyTypeDevice:
			folder := binary.BigEndian.Uint32(key[1:])
			device := binary.BigEndian.Uint32(key[1+4:])
			name := nulString(key[1+4+4:])
			fmt.Printf("[device] F:%d D:%d N:%q", folder, device, name)

			var f protocol.FileInfo
			err := f.Unmarshal(it.Value())
			if err != nil {
				log.Fatal(err)
			}
			fmt.Printf(" V:%v\n", f)

		case db.KeyTypeGlobal:
			folder := binary.BigEndian.Uint32(key[1:])
			name := nulString(key[1+4:])
			var flv db.VersionList
			flv.Unmarshal(it.Value())
			fmt.Printf("[global] F:%d N:%q V:%s\n", folder, name, flv)

		case db.KeyTypeBlock:
			folder := binary.BigEndian.Uint32(key[1:])
			hash := key[1+4 : 1+4+32]
			name := nulString(key[1+4+32:])
			fmt.Printf("[block] F:%d H:%x N:%q I:%d\n", folder, hash, name, binary.BigEndian.Uint32(it.Value()))

		case db.KeyTypeDeviceStatistic:
			fmt.Printf("[dstat] K:%x V:%x\n", it.Key(), it.Value())

		case db.KeyTypeFolderStatistic:
			fmt.Printf("[fstat] K:%x V:%x\n", it.Key(), it.Value())

		case db.KeyTypeVirtualMtime:
			folder := binary.BigEndian.Uint32(key[1:])
			name := nulString(key[1+4:])
			val := it.Value()
			var real, virt time.Time
			real.UnmarshalBinary(val[:len(val)/2])
			virt.UnmarshalBinary(val[len(val)/2:])
			fmt.Printf("[mtime] F:%d N:%q R:%v V:%v\n", folder, name, real, virt)

		case db.KeyTypeFolderIdx:
			key := binary.BigEndian.Uint32(it.Key()[1:])
			fmt.Printf("[folderidx] K:%d V:%q\n", key, it.Value())

		case db.KeyTypeDeviceIdx:
			key := binary.BigEndian.Uint32(it.Key()[1:])
			val := it.Value()
			if len(val) == 0 {
				fmt.Printf("[deviceidx] K:%d V:<nil>\n", key)
			} else {
				dev := protocol.DeviceIDFromBytes(val)
				fmt.Printf("[deviceidx] K:%d V:%s\n", key, dev)
			}

		case db.KeyTypeIndexID:
			device := binary.BigEndian.Uint32(it.Key()[1:])
			folder := binary.BigEndian.Uint32(it.Key()[5:])
			fmt.Printf("[indexid] D:%d F:%d I:%x\n", device, folder, it.Value())

		case db.KeyTypeFolderMeta:
			folder := binary.BigEndian.Uint32(it.Key()[1:])
			fmt.Printf("[foldermeta] F:%d V:%x\n", folder, it.Value())

		case db.KeyTypeMiscData:
			fmt.Printf("[miscdata] K:%q V:%q\n", it.Key()[1:], it.Value())

		case db.KeyTypeSequence:
			folder := binary.BigEndian.Uint32(it.Key()[1:])
			seq := binary.BigEndian.Uint64(it.Key()[5:])
			fmt.Printf("[sequence] F:%d S:%d V:%q\n", folder, seq, it.Value())

		case db.KeyTypeNeed:
			folder := binary.BigEndian.Uint32(it.Key()[1:])
			file := string(it.Key()[5:])
			fmt.Printf("[need] F:%d V:%q\n", folder, file)

		case db.KeyTypeBlockList:
			fmt.Printf("[blocklist] H:%x\n", it.Key()[1:])

		default:
			fmt.Printf("[???]\n  %x\n  %x\n", it.Key(), it.Value())
		}
	}
}
