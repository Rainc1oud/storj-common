// Copyright (C) 2019 Storj Labs, Inc.
// See LICENSE for copying information.

package stripecoinpayments

import (
	"context"
	"time"

	"github.com/skyrings/skyring-common/tools/uuid"
	"github.com/stripe/stripe-go"

	"storj.io/storj/private/date"
	"storj.io/storj/private/memory"
	"storj.io/storj/satellite/payments"
)

// ensures that accounts implements payments.Accounts.
var _ payments.Accounts = (*accounts)(nil)

// accounts is an implementation of payments.Accounts.
type accounts struct {
	service *Service
}

// CreditCards exposes all needed functionality to manage account credit cards.
func (accounts *accounts) CreditCards() payments.CreditCards {
	return &creditCards{service: accounts.service}
}

// Invoices exposes all needed functionality to manage account invoices.
func (accounts *accounts) Invoices() payments.Invoices {
	return &invoices{service: accounts.service}
}

// Setup creates a payment account for the user.
// If account is already set up it will return nil.
func (accounts *accounts) Setup(ctx context.Context, userID uuid.UUID, email string) (err error) {
	defer mon.Task()(&ctx, userID, email)(&err)

	_, err = accounts.service.db.Customers().GetCustomerID(ctx, userID)
	if err == nil {
		return nil
	}

	params := &stripe.CustomerParams{
		Email: stripe.String(email),
	}

	customer, err := accounts.service.stripeClient.Customers.New(params)
	if err != nil {
		return Error.Wrap(err)
	}

	// TODO: delete customer from stripe, if db insertion fails
	return Error.Wrap(accounts.service.db.Customers().Insert(ctx, userID, customer.ID))
}

// Balance returns an integer amount in cents that represents the current balance of payment account.
func (accounts *accounts) Balance(ctx context.Context, userID uuid.UUID) (_ int64, err error) {
	defer mon.Task()(&ctx, userID)(&err)

	customerID, err := accounts.service.db.Customers().GetCustomerID(ctx, userID)
	if err != nil {
		return 0, Error.Wrap(err)
	}

	c, err := accounts.service.stripeClient.Customers.Get(customerID, nil)
	if err != nil {
		return 0, Error.Wrap(err)
	}

	return c.Balance, nil
}

// ProjectCharges returns how much money current user will be charged for each project.
func (accounts *accounts) ProjectCharges(ctx context.Context, userID uuid.UUID) (charges []payments.ProjectCharge, err error) {
	defer mon.Task()(&ctx, userID)(&err)

	// to return empty slice instead of nil if there are no projects
	charges = make([]payments.ProjectCharge, 0)

	projects, err := accounts.service.projectsDB.GetOwn(ctx, userID)
	if err != nil {
		return nil, Error.Wrap(err)
	}

	start, end := date.MonthBoundary(time.Now().UTC())

	for _, project := range projects {
		usage, err := accounts.service.usageDB.GetProjectTotal(ctx, project.ID, start, end)
		if err != nil {
			return charges, Error.Wrap(err)
		}

		charges = append(charges, payments.ProjectCharge{
			ProjectID: project.ID,
			Egress:    usage.Egress / int64(memory.TB) * accounts.service.EgressPrice,
			// TODO: check precision
			ObjectCount:  int64(usage.ObjectCount) * accounts.service.PerObjectPrice,
			StorageGbHrs: int64(usage.Storage) / int64(memory.TB) * accounts.service.TBhPrice,
		})
	}

	return charges, nil
}

// StorjTokens exposes all storj token related functionality.
func (accounts *accounts) StorjTokens() payments.StorjTokens {
	return &storjTokens{service: accounts.service}
}
