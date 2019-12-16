// Copyright (C) 2019 Storj Labs, Inc.
// See LICENSE for copying information.

package satellitedb_test

import (
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
	"github.com/zeebo/errs"
	"go.uber.org/zap/zaptest"

	"storj.io/storj/private/dbutil/dbschema"
	"storj.io/storj/private/dbutil/pgutil"
	"storj.io/storj/private/dbutil/pgutil/pgtest"
	"storj.io/storj/private/dbutil/tempdb"
	"storj.io/storj/private/migrate"
	"storj.io/storj/satellite/satellitedb"
	dbx "storj.io/storj/satellite/satellitedb/dbx"
)

// loadSnapshots loads all the dbschemas from testdata/postgres.* caching the result
func loadSnapshots(connstr string) (*dbschema.Snapshots, error) {
	snapshots := &dbschema.Snapshots{}

	// find all postgres sql files
	matches, err := filepath.Glob("testdata/postgres.*")
	if err != nil {
		return nil, err
	}

	for _, match := range matches {
		versionStr := match[19 : len(match)-4] // hack to avoid trim issues with path differences in windows/linux
		version, err := strconv.Atoi(versionStr)
		if err != nil {
			return nil, errs.New("invalid testdata file %q: %v", match, err)
		}

		scriptData, err := ioutil.ReadFile(match)
		if err != nil {
			return nil, errs.New("could not read testdata file for version %d: %v", version, err)
		}

		snapshot, err := loadSnapshotFromSQL(connstr, string(scriptData))
		if err != nil {
			if pqErr, ok := err.(*pq.Error); ok && pqErr.Detail != "" {
				return nil, fmt.Errorf("Version %d error: %v\nDetail: %s\nHint: %s", version, pqErr, pqErr.Detail, pqErr.Hint)
			}
			return nil, fmt.Errorf("Version %d error: %+v", version, err)
		}
		snapshot.Version = version

		snapshots.Add(snapshot)
	}

	snapshots.Sort()

	return snapshots, nil
}

// loadSnapshotFromSQL inserts script into connstr and loads schema.
func loadSnapshotFromSQL(connstr, script string) (_ *dbschema.Snapshot, err error) {
	db, err := tempdb.OpenUnique(connstr, "load-schema")
	if err != nil {
		return nil, err
	}
	defer func() { err = errs.Combine(err, db.Close()) }()

	_, err = db.Exec(script)
	if err != nil {
		return nil, err
	}

	snapshot, err := pgutil.QuerySnapshot(db)
	if err != nil {
		return nil, err
	}

	snapshot.Script = script
	return snapshot, nil
}

const newDataSeparator = `-- NEW DATA --`

func newData(snap *dbschema.Snapshot) string {
	tokens := strings.SplitN(snap.Script, newDataSeparator, 2)
	if len(tokens) != 2 {
		return ""
	}
	return tokens[1]
}

// loadDBXSChema loads dbxscript schema only once and caches it,
// it shouldn't change during the test
func loadDBXSchema(connstr, dbxscript string) (*dbschema.Schema, error) {
	return loadSchemaFromSQL(connstr, dbxscript)
}

// loadSchemaFromSQL inserts script into connstr and loads schema.
func loadSchemaFromSQL(connstr, script string) (_ *dbschema.Schema, err error) {
	db, err := tempdb.OpenUnique(connstr, "load-schema")
	if err != nil {
		return nil, err
	}
	defer func() { err = errs.Combine(err, db.Close()) }()

	_, err = db.Exec(script)
	if err != nil {
		return nil, err
	}

	return pgutil.QuerySchema(db)
}

func TestMigratePostgres(t *testing.T) {
	if *pgtest.ConnStr == "" {
		t.Skip("Postgres flag missing, example: -postgres-test-db=" + pgtest.DefaultConnStr)
	}

	pgMigrateTest(t, *pgtest.ConnStr)
}

// satelliteDB provides access to certain methods on a *satellitedb.satelliteDB
// instance, since that type is not exported.
type satelliteDB interface {
	TestDBAccess() *dbx.DB
	PostgresMigration() *migrate.Migration
}

func pgMigrateTest(t *testing.T, connStr string) {
	log := zaptest.NewLogger(t)

	snapshots, err := loadSnapshots(connStr)
	require.NoError(t, err)

	// create tempDB
	tempDB, err := tempdb.OpenUnique(connStr, "migrate")
	require.NoError(t, err)
	defer func() { require.NoError(t, tempDB.Close()) }()

	// create a new satellitedb connection
	db, err := satellitedb.New(log, tempDB.ConnStr)
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()

	// we need raw database access unfortunately
	rawdb := db.(satelliteDB).TestDBAccess()

	var finalSchema *dbschema.Schema

	// get migration for this database
	migrations := db.(satelliteDB).PostgresMigration()
	for i, step := range migrations.Steps {
		tag := fmt.Sprintf("#%d - v%d", i, step.Version)

		// run migration up to a specific version
		err := migrations.TargetVersion(step.Version).Run(log.Named("migrate"))
		require.NoError(t, err, tag)

		// find the matching expected version
		expected, ok := snapshots.FindVersion(step.Version)
		require.True(t, ok, "Missing snapshot v%d. Did you forget to add a snapshot for the new migration?", step.Version)

		// insert data for new tables
		if newdata := newData(expected); newdata != "" {
			_, err = rawdb.Exec(newdata)
			require.NoError(t, err, tag)
		}

		// load schema from database
		currentSchema, err := pgutil.QuerySchema(rawdb)
		require.NoError(t, err, tag)

		// we don't care changes in versions table
		currentSchema.DropTable("versions")

		// load data from database
		currentData, err := pgutil.QueryData(rawdb, currentSchema)
		require.NoError(t, err, tag)

		// verify schema and data
		require.Equal(t, expected.Schema, currentSchema, tag)
		require.Equal(t, expected.Data, currentData, tag)

		// keep the last version around
		finalSchema = currentSchema
	}

	// verify that we also match the dbx version
	dbxschema, err := loadDBXSchema(connStr, rawdb.Schema())
	require.NoError(t, err)

	require.Equal(t, dbxschema, finalSchema, "dbx")
}
