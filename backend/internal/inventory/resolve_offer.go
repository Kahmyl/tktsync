package inventory

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
)

type ResolvedOffer struct {
	OfferID            string
	InventoryKind      string
	InventoryID        uuid.UUID
	SourceKind         OfferSourceKind
	SourceAllocationID *uuid.UUID
	AvailableQuantity  int
}

func (s *Service) ResolvePartnerOffer(
	ctx context.Context,
	partnerID uuid.UUID,
	eventID uuid.UUID,
	rawOfferID string,
) (ResolvedOffer, error) {
	rawOfferID = strings.TrimSpace(rawOfferID)

	if rawOfferID == "" {
		return ResolvedOffer{}, apierror.New(
			apierror.CodeValidation,
			"offer_id is required",
		)
	}

	availability, err := s.PartnerAvailability(
		ctx,
		partnerID,
		eventID,
	)
	if err != nil {
		return ResolvedOffer{}, err
	}

	for _, item := range availability.ReservedUnits {
		if item.Offer == nil ||
			item.Offer.OfferID != rawOfferID {
			continue
		}

		result := ResolvedOffer{
			OfferID:           rawOfferID,
			InventoryKind:     "RESERVED",
			InventoryID:       item.InventoryID,
			SourceKind:        item.Offer.SourceKind,
			AvailableQuantity: item.Offer.AvailableQuantity,
		}

		if item.Offer.SourceKind == OfferSourceAllocation {
			allocationID, err := s.allocationForOfferSource(
				ctx,
				"RESERVED",
				item.Offer.SourceID,
			)
			if err != nil {
				return ResolvedOffer{}, err
			}

			result.SourceAllocationID = &allocationID
		}

		return result, nil
	}

	for _, pool := range availability.GAPools {
		for _, offer := range pool.Offers {
			if offer.OfferID != rawOfferID {
				continue
			}

			result := ResolvedOffer{
				OfferID:           rawOfferID,
				InventoryKind:     "GA",
				InventoryID:       pool.InventoryID,
				SourceKind:        offer.SourceKind,
				AvailableQuantity: offer.AvailableQuantity,
			}

			if offer.SourceKind == OfferSourceAllocation {
				allocationID, err := s.allocationForOfferSource(
					ctx,
					"GA",
					offer.SourceID,
				)
				if err != nil {
					return ResolvedOffer{}, err
				}

				result.SourceAllocationID = &allocationID
			}

			return result, nil
		}
	}

	return ResolvedOffer{}, apierror.New(
		apierror.CodeInventoryUnavailable,
		"Offer is stale, invalid, or no longer available",
	)
}

func (s *Service) allocationForOfferSource(
	ctx context.Context,
	inventoryKind string,
	sourceID uuid.UUID,
) (uuid.UUID, error) {
	var allocationID uuid.UUID
	var err error

	switch inventoryKind {
	case "RESERVED":
		err = s.db.QueryRow(
			ctx,
			`
				SELECT allocation_id
				FROM allocation_reserved_units
				WHERE id = $1
				  AND released_at IS NULL
			`,
			sourceID,
		).Scan(&allocationID)

	case "GA":
		err = s.db.QueryRow(
			ctx,
			`
				SELECT allocation_id
				FROM ga_allocation_buckets
				WHERE id = $1
			`,
			sourceID,
		).Scan(&allocationID)

	default:
		return uuid.Nil, apierror.New(
			apierror.CodeInternal,
			"Offer has an unsupported inventory kind",
		)
	}

	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, apierror.New(
			apierror.CodeInventoryUnavailable,
			"Offer source is no longer available",
		)
	}

	if err != nil {
		return uuid.Nil, err
	}

	return allocationID, nil
}
