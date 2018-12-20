// Copyright (C) 2018 Storj Labs, Inc.
// See LICENSE for copying information.

package certificates

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/zeebo/errs"

	"storj.io/storj/internal/testcontext"
	"storj.io/storj/pkg/utils"
	"storj.io/storj/storage"
)

func TestCertSignerConfig_NewAuthDB(t *testing.T) {
	ctx := testcontext.New(t)
	authDB, err := newTestAuthDB(ctx)
	if !assert.NoError(t, err) {
		t.Fatal(err)
	}
	defer func() {
		_ = authDB.Close()
		ctx.Cleanup()
	}()

	assert.NotNil(t, authDB)
	assert.NotNil(t, authDB.DB)
}

func TestAuthorizationDB_Create(t *testing.T) {
	ctx := testcontext.New(t)
	authDB, err := newTestAuthDB(ctx)
	if !assert.NoError(t, err) {
		t.Fatal(err)
	}
	defer func() {
		_ = authDB.Close()
		ctx.Cleanup()
	}()

	cases := []struct {
		testID,
		email string
		startCount,
		incCount,
		newCount,
		endCount int
		errClass *errs.Class
		err      error
	}{
		{
			"first authorization",
			"user1@example.com",
			0, 1, 1, 1,
			nil, nil,
		},
		{
			"second authorization",
			"user1@example.com",
			1, 2, 2, 3,
			nil, nil,
		},
		{
			"large authorization",
			"user2@example.com",
			0, 5, 5, 5,
			nil, nil,
		},
		{
			"authorization error",
			"user2@example.com",
			5, -1, 0, 5,
			&ErrAuthorizationDB, ErrAuthorizationCount,
		},
	}

	for _, c := range cases {
		t.Run(c.testID, func(t *testing.T) {
			emailKey := storage.Key(c.email)

			if c.startCount == 0 {
				_, err = authDB.DB.Get(emailKey)
				assert.Error(t, err)
			} else {
				v, err := authDB.DB.Get(emailKey)
				assert.NoError(t, err)
				assert.NotEmpty(t, v)

				var existingAuths Authorizations
				err = existingAuths.Unmarshal(v)
				assert.NoError(t, err)
				if !assert.Len(t, existingAuths, c.startCount) {
					t.FailNow()
				}
			}

			expectedAuths, err := authDB.Create(c.email, c.incCount)
			if c.errClass != nil {
				assert.True(t, c.errClass.Has(err))
			}
			if c.err != nil {
				assert.Equal(t, c.err, err)
			}
			if c.errClass == nil && c.err == nil {
				assert.NoError(t, err)
			}
			assert.Len(t, expectedAuths, c.newCount)

			v, err := authDB.DB.Get(emailKey)
			assert.NoError(t, err)
			assert.NotEmpty(t, v)

			var actualAuths Authorizations
			err = actualAuths.Unmarshal(v)
			assert.NoError(t, err)
			assert.Len(t, actualAuths, c.endCount)
		})
	}
}

func TestAuthorizationDB_Get(t *testing.T) {
	ctx := testcontext.New(t)
	authDB, err := newTestAuthDB(ctx)
	if !assert.NoError(t, err) {
		t.Fatal(err)
	}
	defer func() {
		_ = authDB.Close()
		ctx.Cleanup()
	}()

	var expectedAuths Authorizations
	for i := 0; i < 5; i++ {
		expectedAuths = append(expectedAuths, &Authorization{
			Token: [tokenLength]byte{1, 2, 3},
		})
	}
	authsBytes, err := expectedAuths.Marshal()
	if !assert.NoError(t, err) {
		t.Fatal(err)
	}
	err = authDB.DB.Put(storage.Key("user@example.com"), authsBytes)
	if !assert.NoError(t, err) {
		t.Fatal(err)
	}

	cases := []struct {
		testID,
		email string
		result Authorizations
	}{
		{
			"Non-existant email",
			"nouser@example.com",
			nil,
		},
		{
			"Exising email",
			"user@example.com",
			expectedAuths,
		},
	}

	for _, c := range cases {
		t.Run(c.testID, func(t *testing.T) {
			auths, err := authDB.Get(c.email)
			assert.NoError(t, err)
			if c.result != nil {
				assert.NotEmpty(t, auths)
				assert.Len(t, auths, len(c.result))
			} else {
				assert.Empty(t, auths)
			}
		})
	}
}

func TestNewAuthorization(t *testing.T) {
	auth, err := NewAuthorization()
	assert.NoError(t, err)
	assert.NotNil(t, auth)
	assert.NotZero(t, auth.Token)
}

func TestAuthorizations_Marshal(t *testing.T) {
	t1 := [tokenLength]byte{1, 2, 3}
	t2 := [tokenLength]byte{4, 5, 6}
	expectedAuths := Authorizations{
		{Token: t1},
		{Token: t2},
	}

	authsBytes, err := expectedAuths.Marshal()
	assert.NoError(t, err)
	assert.NotEmpty(t, authsBytes)

	var actualAuths Authorizations
	decoder := gob.NewDecoder(bytes.NewBuffer(authsBytes))
	err = decoder.Decode(&actualAuths)
	assert.NoError(t, err)
	assert.NotNil(t, actualAuths)
	assert.Equal(t, expectedAuths, actualAuths)
}

func TestAuthorizations_Unmarshal(t *testing.T) {
	t1 := [tokenLength]byte{1, 2, 3}
	t2 := [tokenLength]byte{4, 5, 6}
	expectedAuths := Authorizations{
		{Token: t1},
		{Token: t2},
	}

	authsBytes, err := expectedAuths.Marshal()
	assert.NoError(t, err)
	assert.NotEmpty(t, authsBytes)

	var actualAuths Authorizations
	err = actualAuths.Unmarshal(authsBytes)
	assert.NoError(t, err)
	assert.NotNil(t, actualAuths)
	assert.Equal(t, expectedAuths, actualAuths)
}

func TestAuthorizations_Group(t *testing.T) {
	t1 := [tokenLength]byte{1, 2, 3}
	t2 := [tokenLength]byte{4, 5, 6}
	auths := make(Authorizations, 10)
	for i := 0; i < 10; i++ {
		if i%2 == 0 {
			auths[i] = &Authorization{
				Token: t1,
				Claim: &Claim{
					Timestamp: time.Now().Unix(),
				},
			}
		} else {
			auths[i] = &Authorization{
				Token: t2,
			}
		}
	}

	claimed, open := auths.Group()
	for _, a := range claimed {
		assert.NotNil(t, a.Claim)
	}
	for _, a := range open {
		assert.Nil(t, a.Claim)
	}
}

func TestAuthorizationDB_Emails(t *testing.T) {
	ctx := testcontext.New(t)
	authDB, err := newTestAuthDB(ctx)
	if !assert.NoError(t, err) {
		t.Fatal(err)
	}
	defer func() {
		_ = authDB.Close()
		ctx.Cleanup()
	}()

	var authErrs utils.ErrorGroup
	for i := 0; i < 5; i++ {
		_, err := authDB.Create(fmt.Sprintf("user%d@example.com", i), 1)
		if err != nil {
			authErrs.Add(err)
		}
	}
	err = authErrs.Finish()
	if !assert.NoError(t, err) {
		t.Fatal(err)
	}

	emails, err := authDB.Emails()
	assert.NoError(t, err)
	assert.NotEmpty(t, emails)
}

func newTestAuthDB(ctx *testcontext.Context) (*AuthorizationDB, error) {
	dbPath := "bolt://" + filepath.Join(ctx.Dir(), "authorizations.db")
	config := CertSignerConfig{
		AuthorizationDBURL: dbPath,
	}
	return config.NewAuthDB()
}
