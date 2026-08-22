package idempotency

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/tktsync/tktsync/backend/internal/platform/apierror"
)

type ScopeKind string

const (
	ScopePartner      ScopeKind = "PARTNER"
	ScopeUser         ScopeKind = "USER"
	ScopeBuyerSession ScopeKind = "BUYER_SESSION"
)

type Scope struct {
	Kind ScopeKind
	ID   uuid.UUID
}

type QueryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type Store struct{}

type operationIDContextKey struct{}

func WithOperationID(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, operationIDContextKey{}, id)
}

func OperationIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(operationIDContextKey{}).(uuid.UUID)
	return id, ok && id != uuid.Nil
}

type Claim struct {
	ID     uuid.UUID
	Owner  bool
	Replay *Result
}

type Result struct {
	ExecutionState string
	Code           string
	EntityType     string
	EntityID       *uuid.UUID
	Payload        json.RawMessage
}

// ErrPersistedInProgress means an IN_PROGRESS claim was observed as committed
// database state. Normal TktSync execution claims and completes idempotency
// inside the same business transaction, so rollback cannot leave such a row.
// Observing this value therefore fails closed as misuse/corrupt legacy state.
var ErrPersistedInProgress = errors.New(
	"idempotency operation unexpectedly persisted IN_PROGRESS",
)

// Fingerprint hashes a request representation that the caller has already
// normalized deterministically. Raw map iteration, transport formatting, or
// other unstable serialization must not be passed here.
func Fingerprint(canonical []byte) []byte {
	sum := sha256.Sum256(canonical)
	return append([]byte(nil), sum[:]...)
}

func (Store) Claim(
	ctx context.Context,
	q QueryRower,
	scope Scope,
	operation string,
	key string,
	requestHash []byte,
) (Claim, error) {
	if scope.ID == uuid.Nil {
		return Claim{}, errors.New("idempotency scope ID is required")
	}

	if operation == "" || key == "" || len(requestHash) == 0 {
		return Claim{}, errors.New("idempotency operation, key and request hash are required")
	}

	insert, args, err := claimInsert(scope, operation, key, requestHash)
	if err != nil {
		return Claim{}, err
	}

	var id uuid.UUID
	err = q.QueryRow(ctx, insert, args...).Scan(&id)

	if err == nil {
		return Claim{
			ID:    id,
			Owner: true,
		}, nil
	}

	if !errors.Is(err, pgx.ErrNoRows) {
		return Claim{}, err
	}

	var (
		storedHash    []byte
		execution     string
		resultCode    pgtype.Text
		entityType    pgtype.Text
		entityID      pgtype.UUID
		resultPayload []byte
	)

	err = q.QueryRow(
		ctx,
		`
			SELECT
				id,
				request_hash,
				execution_state,
				result_code,
				result_entity_type,
				result_entity_id,
				result_payload
			FROM idempotency_operations
			WHERE scope_kind = $1
			  AND COALESCE(
			      partner_id,
			      app_user_id,
			      buyer_selection_session_id
			  ) = $2
			  AND operation_type = $3
			  AND idempotency_key = $4
			FOR UPDATE
		`,
		string(scope.Kind),
		scope.ID,
		operation,
		key,
	).Scan(
		&id,
		&storedHash,
		&execution,
		&resultCode,
		&entityType,
		&entityID,
		&resultPayload,
	)
	if err != nil {
		return Claim{}, err
	}

	if len(storedHash) != len(requestHash) ||
		subtle.ConstantTimeCompare(storedHash, requestHash) != 1 {
		return Claim{}, apierror.New(
			apierror.CodeIdempotencyConflict,
			"idempotency key was already used with different request intent",
		)
	}

	if execution == "IN_PROGRESS" {
		return Claim{}, ErrPersistedInProgress
	}

	result := &Result{
		ExecutionState: execution,
		Payload:        append(json.RawMessage(nil), resultPayload...),
	}

	if resultCode.Valid {
		result.Code = resultCode.String
	}

	if entityType.Valid {
		result.EntityType = entityType.String
	}

	if entityID.Valid {
		value := uuid.UUID(entityID.Bytes)
		result.EntityID = &value
	}

	return Claim{
		ID:     id,
		Owner:  false,
		Replay: result,
	}, nil
}

func (Store) CompleteSuccess(
	ctx context.Context,
	q QueryRower,
	id uuid.UUID,
	code string,
	entityType string,
	entityID *uuid.UUID,
	payload any,
) error {
	return complete(
		ctx,
		q,
		id,
		"SUCCEEDED",
		code,
		entityType,
		entityID,
		payload,
	)
}

func (Store) CompleteBusinessFailure(
	ctx context.Context,
	q QueryRower,
	id uuid.UUID,
	code string,
	payload any,
) error {
	if code == "" {
		return errors.New("stable business failure requires result code")
	}

	return complete(
		ctx,
		q,
		id,
		"FAILED_BUSINESS",
		code,
		"",
		nil,
		payload,
	)
}

func complete(
	ctx context.Context,
	q QueryRower,
	id uuid.UUID,
	state string,
	code string,
	entityType string,
	entityID *uuid.UUID,
	payload any,
) error {
	var encoded any

	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal idempotency result payload: %w", err)
		}
		encoded = json.RawMessage(raw)
	}

	var updated uuid.UUID

	err := q.QueryRow(
		ctx,
		`
			UPDATE idempotency_operations
			SET
				execution_state = $2,
				result_code = NULLIF($3, ''),
				result_entity_type = NULLIF($4, ''),
				result_entity_id = $5,
				result_payload = $6,
				completed_at = clock_timestamp()
			WHERE id = $1
			  AND execution_state = 'IN_PROGRESS'
			RETURNING id
		`,
		id,
		state,
		code,
		entityType,
		entityID,
		encoded,
	).Scan(&updated)

	if errors.Is(err, pgx.ErrNoRows) {
		return errors.New("idempotency operation is not IN_PROGRESS")
	}

	return err
}

func claimInsert(
	scope Scope,
	operation string,
	key string,
	requestHash []byte,
) (string, []any, error) {
	switch scope.Kind {
	case ScopePartner:
		return `
			INSERT INTO idempotency_operations (
				scope_kind,
				partner_id,
				operation_type,
				idempotency_key,
				request_hash,
				execution_state
			)
			VALUES ('PARTNER', $1, $2, $3, $4, 'IN_PROGRESS')
			ON CONFLICT DO NOTHING
			RETURNING id
		`, []any{
				scope.ID,
				operation,
				key,
				requestHash,
			}, nil

	case ScopeUser:
		return `
			INSERT INTO idempotency_operations (
				scope_kind,
				app_user_id,
				operation_type,
				idempotency_key,
				request_hash,
				execution_state
			)
			VALUES ('USER', $1, $2, $3, $4, 'IN_PROGRESS')
			ON CONFLICT DO NOTHING
			RETURNING id
		`, []any{
				scope.ID,
				operation,
				key,
				requestHash,
			}, nil

	case ScopeBuyerSession:
		return `
			INSERT INTO idempotency_operations (
				scope_kind,
				buyer_selection_session_id,
				operation_type,
				idempotency_key,
				request_hash,
				execution_state
			)
			VALUES ('BUYER_SESSION', $1, $2, $3, $4, 'IN_PROGRESS')
			ON CONFLICT DO NOTHING
			RETURNING id
		`, []any{
				scope.ID,
				operation,
				key,
				requestHash,
			}, nil

	default:
		return "", nil, fmt.Errorf("unsupported idempotency scope %q", scope.Kind)
	}
}
