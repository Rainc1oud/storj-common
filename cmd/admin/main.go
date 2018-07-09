// Copyright (C) 2018 Storj Labs, Inc.
// See LICENSE for copying information.

package main

import (
	"log"

	"storj.io/storj/pkg/overlay"
	"storj.io/storj/pkg/pointerdb"
	"storj.io/storj/pkg/process"
)

func main() {
	err := process.Main(process.ConfigEnvironment, &overlay.Service{}, &pointerdb.Service{})
	if err != nil {
		log.Fatal(err)
	}
}
