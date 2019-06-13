// Copyright (C) 2019 Storj Labs, Inc.
// See LICENSE for copying information.

package main

// #include "uplink_definitions.h"
import "C"

import "unsafe"

// CString exposes C.String for testing
func CString(s string) *C.char { return C.CString(s) }

// CFree exposes C.free for testing
func CFree(ptr unsafe.Pointer) { C.free(ptr) }

// C types
type Cpchar = *C.char

// Struct types
type CUplinkConfig = C.UplinkConfig

//export internal_UniverseIsEmpty
// internal_UniverseIsEmpty returns true if nothing is stored in the global map.
func internal_UniverseIsEmpty() bool {
	return universe.Empty()
}
