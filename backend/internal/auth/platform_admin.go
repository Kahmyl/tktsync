package auth

import (
	"context"

	"github.com/google/uuid"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
)

func (a *Authorizer) RequirePlatformAdmin(
	ctx context.Context,
	userID uuid.UUID,
) error {
	var allowed bool

	err := a.db.QueryRow(
		ctx,
		`
			SELECT
				EXISTS (
					SELECT 1
					FROM app_users
					WHERE id = $1
					  AND state = 'ACTIVE'
				)
				AND EXISTS (
					SELECT 1
					FROM platform_user_roles
					WHERE user_id = $1
					  AND role = 'PLATFORM_ADMIN'
				)
		`,
		userID,
	).Scan(&allowed)
	if err != nil {
		return err
	}

	if !allowed {
		return apierror.New(
			apierror.CodeNotAuthorized,
			"operation is not authorized",
		)
	}

	return nil
}
