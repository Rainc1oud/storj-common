// Copyright (C) 2018 Storj Labs, Inc.
// See LICENSE for copying information.

package satellite

// DB contains access to different satellite databases
type DB interface {
	// Users is a getter for Users repository
	Users() Users
	// Companies is a getter for Companies repository
	Companies() Companies
	// Projects is a getter for Projects repository
	Projects() Projects
	// ProjectMembers is a getter for ProjectMembers repository
	ProjectMembers() ProjectMembers

	// CreateTables is a method for creating all tables for satellitedb
	CreateTables() error
	// Close is used to close db connection
	Close() error
}
