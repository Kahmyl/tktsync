package auth

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
)

type HumanPrincipal struct {
	Provider string
	Subject  string
}

type AppUser struct {
	ID    uuid.UUID
	State string
}

type Authorizer struct {
	db QueryRower
}

func NewAuthorizer(db QueryRower) *Authorizer {
	return &Authorizer{db: db}
}

func (a *Authorizer) ResolveHuman(
	ctx context.Context,
	principal HumanPrincipal,
) (AppUser, error) {
	var user AppUser

	err := a.db.QueryRow(
		ctx,
		`
			SELECT id, state
			FROM app_users
			WHERE auth_provider = $1
			  AND auth_subject = $2
		`,
		principal.Provider,
		principal.Subject,
	).Scan(&user.ID, &user.State)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return AppUser{}, apierror.New(
				apierror.CodeNotAuthorized,
				"user is not authorized",
			)
		}
		return AppUser{}, err
	}

	if user.State != "ACTIVE" {
		return AppUser{}, apierror.New(
			apierror.CodeNotAuthorized,
			"user is disabled",
		)
	}

	return user, nil
}

func (a *Authorizer) RequireHumanEventRole(
	ctx context.Context,
	userID uuid.UUID,
	eventID uuid.UUID,
	allowedRoles ...string,
) error {
	var (
		userActive    bool
		platformAdmin bool
		eventRole     bool
	)

	err := a.db.QueryRow(
		ctx,
		`
			SELECT
				EXISTS (
					SELECT 1
					FROM app_users
					WHERE id = $1
					  AND state = 'ACTIVE'
				),
				EXISTS (
					SELECT 1
					FROM platform_user_roles
					WHERE user_id = $1
					  AND role = 'PLATFORM_ADMIN'
				),
				EXISTS (
					SELECT 1
					FROM event_staff_assignments
					WHERE user_id = $1
					  AND event_id = $2
					  AND state = 'ACTIVE'
					  AND role = ANY($3::text[])
				)
		`,
		userID,
		eventID,
		allowedRoles,
	).Scan(
		&userActive,
		&platformAdmin,
		&eventRole,
	)
	if err != nil {
		return err
	}

	if !userActive || (!platformAdmin && !eventRole) {
		return apierror.New(
			apierror.CodeNotAuthorized,
			"operation is not authorized",
		)
	}

	return nil
}

func (a *Authorizer) RequireNewPartnerAcquisition(
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
			SELECT p.state, pea.state
			FROM partners p
			LEFT JOIN partner_event_access pea
			  ON pea.partner_id = p.id
			 AND pea.event_id = $2
			WHERE p.id = $1
		`,
		principal.PartnerID,
		eventID,
	).Scan(&partnerState, &accessState)
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
