package adminapi

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
	"github.com/tktsync/tktsync/backend/internal/platform/publicid"
)

func (h *Handler) venueResponse(
	ctx context.Context,
	id uuid.UUID,
) (map[string]any, error) {
	var (
		name        string
		addressText *string
		metadata    []byte
		createdAt   any
		updatedAt   any
	)

	err := h.db.QueryRow(
		ctx,
		`
			SELECT
				name,
				address_text,
				metadata,
				created_at,
				updated_at
			FROM venues
			WHERE id = $1
		`,
		id,
	).Scan(
		&name,
		&addressText,
		&metadata,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apierror.New(
				apierror.CodeResourceNotFound,
				"venue not found",
			)
		}
		return nil, err
	}

	return map[string]any{
		"id":           publicid.Encode(publicid.Venue, id),
		"name":         name,
		"address_text": addressText,
		"metadata":     rawJSON(metadata),
		"created_at":   createdAt,
		"updated_at":   updatedAt,
	}, nil
}

func (h *Handler) eventResponse(
	ctx context.Context,
	id uuid.UUID,
) (map[string]any, error) {
	var (
		venueID          uuid.UUID
		name             string
		state            string
		startsAt         any
		endsAt           any
		salesOpenAt      any
		salesCloseAt     any
		admissionOpenAt  any
		admissionCloseAt any
		timezoneName     *string
		createdAt        any
		updatedAt        any
	)

	err := h.db.QueryRow(
		ctx,
		`
			SELECT
				venue_id,
				name,
				state,
				starts_at,
				ends_at,
				sales_open_at,
				sales_close_at,
				admission_open_at,
				admission_close_at,
				timezone_name,
				created_at,
				updated_at
			FROM events
			WHERE id = $1
		`,
		id,
	).Scan(
		&venueID,
		&name,
		&state,
		&startsAt,
		&endsAt,
		&salesOpenAt,
		&salesCloseAt,
		&admissionOpenAt,
		&admissionCloseAt,
		&timezoneName,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apierror.New(
				apierror.CodeResourceNotFound,
				"event not found",
			)
		}
		return nil, err
	}

	return map[string]any{
		"id":                 publicid.Encode(publicid.Event, id),
		"venue_id":           publicid.Encode(publicid.Venue, venueID),
		"name":               name,
		"state":              state,
		"starts_at":          startsAt,
		"ends_at":            endsAt,
		"sales_open_at":      salesOpenAt,
		"sales_close_at":     salesCloseAt,
		"admission_open_at":  admissionOpenAt,
		"admission_close_at": admissionCloseAt,
		"timezone_name":      timezoneName,
		"created_at":         createdAt,
		"updated_at":         updatedAt,
	}, nil
}
