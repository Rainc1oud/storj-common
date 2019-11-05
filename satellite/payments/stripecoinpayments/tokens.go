// Copyright (C) 2019 Storj Labs, Inc.
// See LICENSE for copying information.

package stripecoinpayments

import (
	"context"
	"math/big"

	"github.com/skyrings/skyring-common/tools/uuid"

	"storj.io/storj/satellite/payments"
	"storj.io/storj/satellite/payments/coinpayments"
)

// ensure that storjTokens implements payments.StorjTokens.
var _ payments.StorjTokens = (*storjTokens)(nil)

// storjTokens implements payments.StorjTokens.
type storjTokens struct {
	service *Service
}

// Deposit creates new deposit transaction with the given amount returning
// ETH wallet address where funds should be sent. There is one
// hour limit to complete the transaction. Transaction is saved to DB with
// reference to the user who made the deposit.
func (tokens *storjTokens) Deposit(ctx context.Context, userID uuid.UUID, amount big.Float) (_ *payments.Transaction, err error) {
	defer mon.Task()(&ctx, userID, amount)(&err)

	customerID, err := tokens.service.db.Customers().GetCustomerID(ctx, userID)
	if err != nil {
		return nil, Error.Wrap(err)
	}

	c, err := tokens.service.stripeClient.Customers.Get(customerID, nil)
	if err != nil {
		return nil, Error.Wrap(err)
	}

	tx, err := tokens.service.coinPayments.Transactions().Create(ctx,
		coinpayments.CreateTX{
			Amount:      amount,
			CurrencyIn:  coinpayments.CurrencyLTCT,
			CurrencyOut: coinpayments.CurrencyLTCT,
			BuyerEmail:  c.Email,
		},
	)
	if err != nil {
		return nil, Error.Wrap(err)
	}

	key, err := coinpayments.GetTransacationKeyFromURL(tx.CheckoutURL)
	if err != nil {
		return nil, Error.Wrap(err)
	}

	cpTX, err := tokens.service.db.Transactions().Insert(ctx,
		Transaction{
			ID:        tx.ID,
			AccountID: userID,
			Address:   tx.Address,
			Amount:    tx.Amount,
			Received:  big.Float{},
			Status:    coinpayments.StatusPending,
			Key:       key,
		},
	)
	if err != nil {
		return nil, Error.Wrap(err)
	}

	return &payments.Transaction{
		ID:        payments.TransactionID(tx.ID),
		AccountID: userID,
		Amount:    tx.Amount,
		Received:  big.Float{},
		Address:   tx.Address,
		Status:    payments.TransactionStatusPending,
		CreatedAt: cpTX.CreatedAt,
	}, nil
}
