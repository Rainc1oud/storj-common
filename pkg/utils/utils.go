// Copyright (C) 2019 Storj Labs, Inc.
// See LICENSE for copying information.

package utils

import (
	"time"

	"github.com/zeebo/errs"
)

// CombineErrors combines multiple errors to a single error
func CombineErrors(errs ...error) error {
	var errlist ErrorGroup
	errlist.Add(errs...)
	return errlist.Finish()
}

type combinedError []error

func (errs combinedError) Cause() error {
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

func (errs combinedError) Error() string {
	if len(errs) > 0 {
		limit := 5
		if len(errs) < limit {
			limit = len(errs)
		}
		allErrors := errs[0].Error()
		for _, err := range errs[1:limit] {
			allErrors += "\n" + err.Error()
		}
		return allErrors
	}
	return ""
}

// ErrorGroup contains a set of non-nil errors
type ErrorGroup errs.Group

// Add adds an error to the ErrorGroup if it is non-nil
func (e *ErrorGroup) Add(errrs ...error) {
	(*errs.Group)(e).Add(errrs...)
}

// Finish returns nil if there were no non-nil errors, the first error if there
// was only one non-nil error, or the result of CombineErrors if there was more
// than one non-nil error.
func (e *ErrorGroup) Finish() error {
	return (*errs.Group)(e).Err()
}

// CollectErrors returns first error from channel and all errors that happen within duration
func CollectErrors(errch chan error, duration time.Duration) error {
	errch = discardNil(errch)
	errs := []error{<-errch}
	timeout := time.After(duration)
	for {
		select {
		case err := <-errch:
			errs = append(errs, err)
		case <-timeout:
			return CombineErrors(errs...)
		}
	}
}

// discard nil errors that are returned from services
func discardNil(ch chan error) chan error {
	r := make(chan error)
	go func() {
		for err := range ch {
			if err == nil {
				continue
			}
			r <- err
		}
		close(r)
	}()
	return r
}
