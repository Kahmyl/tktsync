package adminapi

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tktsync/tktsync/backend/internal/auth"
	"github.com/tktsync/tktsync/backend/internal/idempotency"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
	"github.com/tktsync/tktsync/backend/internal/platform/database"
)

type response struct {
	Status int
	Body   json.RawMessage
}

type persistedResponse struct {
	Status        int             `json:"status"`
	Body          json.RawMessage `json:"body,omitempty"`
	ProtectedBody string          `json:"protected_body,omitempty"`
}

type persistedFailure struct {
	Message    string         `json:"message"`
	Details    map[string]any `json:"details,omitempty"`
	HTTPStatus int            `json:"http_status"`
}

type authorizeFunc func(
	context.Context,
	*auth.Authorizer,
	uuid.UUID,
) error

type mutationFunc func(
	context.Context,
	uuid.UUID,
) (response, error)

func (h *Handler) executeMutation(
	ctx context.Context,
	principal auth.HumanPrincipal,
	idempotencyKey string,
	operation string,
	canonical []byte,
	authorize authorizeFunc,
	protectReplay bool,
	mutate mutationFunc,
) (response, error) {
	if idempotencyKey == "" {
		return response{}, apierror.New(
			apierror.CodeValidation,
			"Idempotency-Key header is required",
		)
	}

	requestHash := idempotency.Fingerprint(
		canonical,
	)

	var (
		finalResponse response
		businessErr   error
	)

	err := h.transactions.Run(
		ctx,
		func(tx pgx.Tx) error {
			authorizer := auth.NewAuthorizer(tx)

			user, err := authorizer.ResolveHuman(
				ctx,
				principal,
			)
			if err != nil {
				return err
			}

			if authorize != nil {
				if err := authorize(
					ctx,
					authorizer,
					user.ID,
				); err != nil {
					return err
				}
			}

			claim, err := h.idempotency.Claim(
				ctx,
				tx,
				idempotency.Scope{
					Kind: idempotency.ScopeUser,
					ID:   user.ID,
				},
				operation,
				idempotencyKey,
				requestHash,
			)
			if err != nil {
				return err
			}

			if !claim.Owner {
				if claim.Replay == nil {
					return errors.New(
						"idempotency replay result is missing",
					)
				}

				if claim.Replay.ExecutionState ==
					"FAILED_BUSINESS" {
					var stored persistedFailure

					if len(claim.Replay.Payload) != 0 {
						if err := json.Unmarshal(
							claim.Replay.Payload,
							&stored,
						); err != nil {
							return err
						}
					}

					replayed := apierror.New(
						apierror.Code(
							claim.Replay.Code,
						),
						stored.Message,
					)

					if stored.HTTPStatus != 0 {
						replayed.HTTPStatus =
							stored.HTTPStatus
					}

					replayed.Details =
						stored.Details

					businessErr = replayed
					return nil
				}

				var stored persistedResponse

				if err := json.Unmarshal(
					claim.Replay.Payload,
					&stored,
				); err != nil {
					return err
				}

				body := stored.Body

				if stored.ProtectedBody != "" {
					decoded, err :=
						h.replayProtector.Unprotect(
							stored.ProtectedBody,
						)
					if err != nil {
						return err
					}

					body = json.RawMessage(decoded)
				}

				finalResponse = response{
					Status: stored.Status,
					Body:   body,
				}

				return nil
			}

			businessTx, err := tx.Begin(ctx)
			if err != nil {
				return err
			}

			businessCtx := database.WithTransaction(
				ctx,
				businessTx,
			)
			businessCtx = idempotency.WithOperationID(businessCtx, claim.ID)

			result, err := mutate(
				businessCtx,
				user.ID,
			)
			if err != nil {
				_ = businessTx.Rollback(ctx)

				apiErr, ok := apierror.As(err)
				if !ok {
					return err
				}

				stored := persistedFailure{
					Message:    apiErr.Message,
					Details:    apiErr.Details,
					HTTPStatus: apiErr.HTTPStatus,
				}

				if err := h.idempotency.
					CompleteBusinessFailure(
						ctx,
						tx,
						claim.ID,
						string(apiErr.Code),
						stored,
					); err != nil {
					return err
				}

				businessErr = apiErr
				return nil
			}

			if err := businessTx.Commit(ctx); err != nil {
				return err
			}

			stored := persistedResponse{
				Status: result.Status,
			}

			if protectReplay {
				if h.replayProtector == nil {
					return apierror.New(
						apierror.
							CodeAuthorityTemporarilyUnavailable,
						"credential replay protection is not configured",
					)
				}

				protected, err :=
					h.replayProtector.Protect(
						result.Body,
					)
				if err != nil {
					return err
				}

				stored.ProtectedBody = protected
			} else {
				stored.Body = result.Body
			}

			if err := h.idempotency.CompleteSuccess(
				ctx,
				tx,
				claim.ID,
				"OK",
				"",
				nil,
				stored,
			); err != nil {
				return err
			}

			finalResponse = result
			return nil
		},
	)

	if err != nil {
		return response{}, err
	}

	if businessErr != nil {
		return response{}, businessErr
	}

	return finalResponse, nil
}
