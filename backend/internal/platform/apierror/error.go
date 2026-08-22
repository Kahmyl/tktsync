package apierror

import (
	"errors"
	"net/http"
)

type Code string

const (
	CodeInventoryUnavailable            Code = "INVENTORY_UNAVAILABLE"
	CodeInsufficientGAQuantity          Code = "INSUFFICIENT_GA_QUANTITY"
	CodeInventoryNotEligibleForPartner  Code = "INVENTORY_NOT_ELIGIBLE_FOR_PARTNER"
	CodeHoldExpired                     Code = "HOLD_EXPIRED"
	CodeHoldNotOwned                    Code = "HOLD_NOT_OWNED"
	CodeReservationNotModifiable        Code = "RESERVATION_NOT_MODIFIABLE"
	CodeCheckoutAlreadyActive           Code = "CHECKOUT_ALREADY_ACTIVE"
	CodeCheckoutWindowExpired           Code = "CHECKOUT_WINDOW_EXPIRED"
	CodePaymentStatusUncertain          Code = "PAYMENT_STATUS_UNCERTAIN"
	CodeReconciliationExpired           Code = "RECONCILIATION_EXPIRED"
	CodeAlreadyConfirmed                Code = "ALREADY_CONFIRMED"
	CodeIdempotencyConflict             Code = "IDEMPOTENCY_CONFLICT"
	CodeEventNotOnSale                  Code = "EVENT_NOT_ON_SALE"
	CodeEventPaused                     Code = "EVENT_PAUSED"
	CodeEventSalesClosed                Code = "EVENT_SALES_CLOSED"
	CodeEventCancelled                  Code = "EVENT_CANCELLED"
	CodePartnerDisabled                 Code = "PARTNER_DISABLED"
	CodePartnerEventAccessDisabled      Code = "PARTNER_EVENT_ACCESS_DISABLED"
	CodeNotAuthorized                   Code = "NOT_AUTHORIZED"
	CodeTicketInvalid                   Code = "TICKET_INVALID"
	CodeTicketVoid                      Code = "TICKET_VOID"
	CodeCredentialRevoked               Code = "CREDENTIAL_REVOKED"
	CodeCredentialSuperseded            Code = "CREDENTIAL_SUPERSEDED"
	CodeTicketAlreadyAdmitted           Code = "TICKET_ALREADY_ADMITTED"
	CodeAdmissionNotOpen                Code = "ADMISSION_NOT_OPEN"
	CodeWrongEvent                      Code = "WRONG_EVENT"
	CodeAuthorityTemporarilyUnavailable Code = "AUTHORITY_TEMPORARILY_UNAVAILABLE"
	CodeCurrencyMismatch                Code = "CURRENCY_MISMATCH"
	CodeValidation                      Code = "VALIDATION_ERROR"
	CodeResourceNotFound                Code = "RESOURCE_NOT_FOUND"
	CodeRateLimited                     Code = "RATE_LIMITED"
	CodeInternal                        Code = "INTERNAL_ERROR"
)

type Error struct {
	Code       Code
	Message    string
	Details    map[string]any
	HTTPStatus int
}

func (e *Error) Error() string {
	return string(e.Code) + ": " + e.Message
}

func New(code Code, message string) *Error {
	return &Error{
		Code:       code,
		Message:    message,
		HTTPStatus: DefaultHTTPStatus(code),
	}
}

func WithStatus(code Code, message string, status int) *Error {
	return &Error{
		Code:       code,
		Message:    message,
		HTTPStatus: status,
	}
}

func WithDetails(code Code, message string, details map[string]any) *Error {
	err := New(code, message)
	err.Details = details
	return err
}

func As(err error) (*Error, bool) {
	var target *Error
	if !errors.As(err, &target) {
		return nil, false
	}
	return target, true
}

func DefaultHTTPStatus(code Code) int {
	switch code {
	case CodeValidation, CodeCurrencyMismatch:
		return http.StatusBadRequest

	case CodeNotAuthorized,
		CodePartnerDisabled,
		CodePartnerEventAccessDisabled:
		return http.StatusForbidden

	case CodeResourceNotFound:
		return http.StatusNotFound

	case CodeRateLimited:
		return http.StatusTooManyRequests

	case CodeAuthorityTemporarilyUnavailable:
		return http.StatusServiceUnavailable

	case CodeInternal:
		return http.StatusInternalServerError

	default:
		return http.StatusConflict
	}
}
