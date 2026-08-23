package reservation

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
)

func translateInventoryWriteError(
	err error,
) error {
	if err == nil {
		return nil
	}

	if strings.Contains(
		err.Error(),
		"reserved_inventory_one_active_claim_uq",
	) {
		return apierror.New(
			apierror.CodeInventoryUnavailable,
			"Reserved inventory was acquired concurrently",
		)
	}

	return err
}

func sortedUUIDSet(
	values map[uuid.UUID]struct{},
) []uuid.UUID {
	result := make(
		[]uuid.UUID,
		0,
		len(values),
	)

	for value := range values {
		result = append(
			result,
			value,
		)
	}

	sort.Slice(
		result,
		func(i, j int) bool {
			return result[i].String() <
				result[j].String()
		},
	)

	return result
}

func nullableUUID(
	value pgtype.UUID,
) *uuid.UUID {
	if !value.Valid {
		return nil
	}

	result := uuid.UUID(
		value.Bytes,
	)

	return &result
}

func nullableTime(
	value pgtype.Timestamptz,
) *time.Time {
	if !value.Valid {
		return nil
	}

	result := value.Time

	return &result
}

func requireSingleCurrency(
	items []resolvedItem,
) (string, error) {
	if len(items) == 0 {
		return "", apierror.New(
			apierror.CodeValidation,
			"Reservation requires inventory",
		)
	}

	currency := items[0].Currency

	for _, item := range items[1:] {
		if item.Currency != currency {
			return "",
				apierror.New(
					apierror.CodeCurrencyMismatch,
					"All Reservation items must use one currency",
				)
		}
	}

	return currency, nil
}

func allocationPartnerMatches(
	info allocationInfo,
	partnerID uuid.UUID,
) bool {
	return info.PartnerID != nil &&
		*info.PartnerID == partnerID
}

func requireAllocationActiveForPartner(
	info allocationInfo,
	partnerID uuid.UUID,
) error {
	if info.State != "ACTIVE" ||
		info.Mode != "CHANNEL" ||
		!allocationPartnerMatches(
			info,
			partnerID,
		) {
		return apierror.New(
			apierror.CodeInventoryNotEligibleForPartner,
			"Inventory Allocation is not eligible for this Partner",
		)
	}

	return nil
}

func internalf(
	format string,
	args ...any,
) error {
	return apierror.New(
		apierror.CodeInternal,
		fmt.Sprintf(
			format,
			args...,
		),
	)
}
