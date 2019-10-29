// Copyright (C) 2019 Storj Labs, Inc.
// See LICENSE for copying information.

package console

import (
	"github.com/zeebo/errs"
)

const (
	passMinLength = 6
)

// ErrValidation validation related error class
var ErrValidation = errs.Class("validation error")

// validationError is slice of ErrValidation class errors
type validationErrors []error

// Add new ErrValidation err
func (validation *validationErrors) Add(format string, args ...interface{}) {
	*validation = append(*validation, ErrValidation.New(format, args...))
}

// AddWrap adds new ErrValidation wrapped err
func (validation *validationErrors) AddWrap(err error) {
	*validation = append(*validation, ErrValidation.Wrap(err))
}

// Combine returns combined validation errors
func (validation *validationErrors) Combine() error {
	return errs.Combine(*validation...)
}

// validatePassword validates password
func validatePassword(pass string) error {
	var errs validationErrors

	if len(pass) < passMinLength {
		errs.Add(passwordIncorrectErrMsg, passMinLength)
	}

	return errs.Combine()
}

// validateFullName validates full name.
func validateFullName(name string) error {
	if name == "" {
		return errs.New("full name can not be empty")
	}

	return nil
}
