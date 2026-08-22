package auth

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
)

func (a *Authorizer) RequirePartnerEventAccess(
	ctx context.Context,
	principal PartnerPrincipal,
	eventID uuid.UUID,
) error {
	var (
		partnerState string
		accessState  *string
	)

	err := a.db.QueryRow(
		ctx,
		`
			SELECT
				p.state,
				pea.state
			FROM partners p
			LEFT JOIN partner_event_access pea
			  ON pea.partner_id = p.id
			 AND pea.event_id = $2
			WHERE p.id = $1
		`,
		principal.PartnerID,
		eventID,
	).Scan(
		&partnerState,
		&accessState,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return apierror.New(
				apierror.CodeNotAuthorized,
				"Partner is not authorized for this Event",
			)
		}

		return err
	}

	if partnerState != "ACTIVE" {
		return apierror.New(
			apierror.CodePartnerDisabled,
			"Partner is disabled",
		)
	}

	if accessState == nil {
		return apierror.New(
			apierror.CodeNotAuthorized,
			"Partner is not authorized for this Event",
		)
	}

	if *accessState != "ACTIVE" {
		return apierror.New(
			apierror.CodePartnerEventAccessDisabled,
			"Partner Event access is disabled",
		)
	}

	return nil
}
