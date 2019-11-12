// Copyright (C) 2019 Storj Labs, Inc.
// See LICENSE for copying information.

package consoleapi

import (
	"encoding/json"
	"io/ioutil"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/zeebo/errs"
	"go.uber.org/zap"
	monkit "gopkg.in/spacemonkeygo/monkit.v2"

	"storj.io/storj/satellite/console"
	"storj.io/storj/satellite/payments"
)

var (
	// ErrPaymentsAPI - console payments api error type.
	ErrPaymentsAPI = errs.Class("console payments api error")
	mon            = monkit.Package()
)

// Payments is an api controller that exposes all payment related functionality
type Payments struct {
	log     *zap.Logger
	service *console.Service
}

// NewPayments is a constructor for api payments controller.
func NewPayments(log *zap.Logger, service *console.Service) *Payments {
	return &Payments{
		log:     log,
		service: service,
	}
}

// SetupAccount creates a payment account for the user.
func (p *Payments) SetupAccount(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var err error
	defer mon.Task()(&ctx)(&err)

	err = p.service.Payments().SetupAccount(ctx)
	if err != nil {
		if console.ErrUnauthorized.Has(err) {
			p.serveJSONError(w, http.StatusUnauthorized, err)
			return
		}

		p.serveJSONError(w, http.StatusInternalServerError, err)
		return
	}
}

// AccountBalance returns an integer amount in cents that represents the current balance of payment account.
func (p *Payments) AccountBalance(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var err error
	defer mon.Task()(&ctx)(&err)

	balance, err := p.service.Payments().AccountBalance(ctx)
	if err != nil {
		if console.ErrUnauthorized.Has(err) {
			p.serveJSONError(w, http.StatusUnauthorized, err)
			return
		}

		p.serveJSONError(w, http.StatusInternalServerError, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(&balance)
	if err != nil {
		p.log.Error("failed to write json balance response", zap.Error(ErrPaymentsAPI.Wrap(err)))
	}
}

// AddCreditCard is used to save new credit card and attach it to payment account.
func (p *Payments) AddCreditCard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var err error
	defer mon.Task()(&ctx)(&err)

	bodyBytes, err := ioutil.ReadAll(r.Body)
	if err != nil {
		p.serveJSONError(w, http.StatusBadRequest, err)
		return
	}

	token := string(bodyBytes)

	err = p.service.Payments().AddCreditCard(ctx, token)
	if err != nil {
		if console.ErrUnauthorized.Has(err) {
			p.serveJSONError(w, http.StatusUnauthorized, err)
			return
		}

		p.serveJSONError(w, http.StatusInternalServerError, err)
		return
	}
}

// ListCreditCards returns a list of credit cards for a given payment account.
func (p *Payments) ListCreditCards(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var err error
	defer mon.Task()(&ctx)(&err)

	cards, err := p.service.Payments().ListCreditCards(ctx)
	if err != nil {
		if console.ErrUnauthorized.Has(err) {
			p.serveJSONError(w, http.StatusUnauthorized, err)
			return
		}

		p.serveJSONError(w, http.StatusInternalServerError, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(cards)
	if err != nil {
		p.log.Error("failed to write json list cards response", zap.Error(ErrPaymentsAPI.Wrap(err)))
	}
}

// MakeCreditCardDefault makes a credit card default payment method.
func (p *Payments) MakeCreditCardDefault(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var err error
	defer mon.Task()(&ctx)(&err)

	cardID, err := ioutil.ReadAll(r.Body)
	if err != nil {
		p.serveJSONError(w, http.StatusBadRequest, err)
		return
	}

	err = p.service.Payments().MakeCreditCardDefault(ctx, string(cardID))
	if err != nil {
		if console.ErrUnauthorized.Has(err) {
			p.serveJSONError(w, http.StatusUnauthorized, err)
			return
		}

		p.serveJSONError(w, http.StatusInternalServerError, err)
		return
	}
}

// RemoveCreditCard is used to detach a credit card from payment account.
func (p *Payments) RemoveCreditCard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var err error
	defer mon.Task()(&ctx)(&err)

	vars := mux.Vars(r)
	cardID := vars["cardId"]

	if cardID == "" {
		p.serveJSONError(w, http.StatusBadRequest, err)
		return
	}

	err = p.service.Payments().RemoveCreditCard(ctx, cardID)
	if err != nil {
		if console.ErrUnauthorized.Has(err) {
			p.serveJSONError(w, http.StatusUnauthorized, err)
			return
		}

		p.serveJSONError(w, http.StatusInternalServerError, err)
		return
	}
}

// BillingHistory returns a list of invoices, transactions and all others billing history items for payment account.
func (p *Payments) BillingHistory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var err error
	defer mon.Task()(&ctx)(&err)

	billingHistory, err := p.service.Payments().BillingHistory(ctx)
	if err != nil {
		if console.ErrUnauthorized.Has(err) {
			p.serveJSONError(w, http.StatusUnauthorized, err)
			return
		}

		p.serveJSONError(w, http.StatusInternalServerError, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(billingHistory)
	if err != nil {
		p.log.Error("failed to write json billing history response", zap.Error(ErrPaymentsAPI.Wrap(err)))
	}
}

// TokenDeposit creates new deposit transaction and info about address and amount of newly created tx.
func (p *Payments) TokenDeposit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var err error

	defer mon.Task()(&ctx)(&err)

	var requestData struct {
		Amount string `json:"amount"`
	}

	if err = json.NewDecoder(r.Body).Decode(&requestData); err != nil {
		p.serveJSONError(w, http.StatusBadRequest, err)
	}

	amount, err := payments.ParseTokenAmount(requestData.Amount)
	if err != nil {
		p.serveJSONError(w, http.StatusBadRequest, err)
	}

	tx, err := p.service.Payments().TokenDeposit(ctx, amount)
	if err != nil {
		if console.ErrUnauthorized.Has(err) {
			p.serveJSONError(w, http.StatusUnauthorized, err)
			return
		}

		p.serveJSONError(w, http.StatusInternalServerError, err)
		return
	}

	var responseData struct {
		Address string `json:"address"`
		Amount  string `json:"amount"`
	}

	responseData.Amount = tx.Amount.String()
	responseData.Address = tx.Address

	err = json.NewEncoder(w).Encode(responseData)
	if err != nil {
		p.log.Error("failed to write json token deposit response", zap.Error(ErrPaymentsAPI.Wrap(err)))
	}
}

// serveJSONError writes JSON error to response output stream.
func (p *Payments) serveJSONError(w http.ResponseWriter, status int, err error) {
	w.WriteHeader(status)

	var response struct {
		Error string `json:"error"`
	}

	response.Error = err.Error()

	err = json.NewEncoder(w).Encode(response)
	if err != nil {
		p.log.Error("failed to write json error response", zap.Error(ErrPaymentsAPI.Wrap(err)))
	}
}
