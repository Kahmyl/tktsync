package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tktsync/tktsync/backend/internal/platform/database"
	"github.com/tktsync/tktsync/backend/internal/platform/publicid"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type DeliveryWorker struct {
	transactions           *database.Runner
	box                    *SecretBox
	client                 *http.Client
	batchSize, maxAttempts int
	lease                  time.Duration
	workerID               uuid.UUID
}

func NewDeliveryWorker(transactions *database.Runner, box *SecretBox, allowPrivate bool, batchSize, maxAttempts int, timeout time.Duration) *DeliveryWorker {
	if batchSize <= 0 {
		batchSize = 50
	}
	if maxAttempts <= 0 {
		maxAttempts = 8
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	dialer := &net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy:                 nil,
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		MaxConnsPerHost:       20,
		ExpectContinueTimeout: time.Second,
		DialContext:           safeDialContext(dialer, allowPrivate),
	}
	return &DeliveryWorker{transactions: transactions, box: box, client: &http.Client{Timeout: timeout, Transport: otelhttp.NewTransport(transport), CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, batchSize: batchSize, maxAttempts: maxAttempts, lease: timeout + 5*time.Second, workerID: uuid.New()}
}

func safeDialContext(dialer *net.Dialer, allowPrivate bool) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		for _, resolved := range ips {
			if !allowPrivate && unsafeIP(resolved.IP) {
				continue
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(resolved.IP.String(), port))
		}
		return nil, errors.New("webhook destination resolves only to disallowed addresses")
	}
}

type claimedDelivery struct {
	ID, FactID    uuid.UUID
	URL, FactType string
	EventID       *uuid.UUID
	AggregateType string
	AggregateID   *uuid.UUID
	Payload       json.RawMessage
	OccurredAt    time.Time
	Attempt       int
	Secrets       [][]byte
}

func (w *DeliveryWorker) RunOnce(ctx context.Context) error {
	for i := 0; i < w.batchSize; i++ {
		delivery, ok, err := w.claim(ctx)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		if err := w.deliver(ctx, delivery); err != nil {
			return err
		}
	}
	return nil
}

func (w *DeliveryWorker) claim(
	ctx context.Context,
) (claimedDelivery, bool, error) {
	var result claimedDelivery
	found := false

	err := w.transactions.Run(
		ctx,
		func(tx pgx.Tx) error {
			for {
				result = claimedDelivery{}

				var endpointState string
				var ciphertexts [][]byte
				var encryptionKeyVersions []int32

				err := tx.QueryRow(
					ctx,
					`
						SELECT
							d.id,
							e.url,
							e.state,
							o.fact_id,
							o.event_id,
							o.fact_type,
							o.aggregate_type,
							o.aggregate_id,
							o.payload,
							o.created_at,
							d.attempt_count,
							ARRAY(
								SELECT
									s.secret_ciphertext
								FROM partner_webhook_signing_secrets s
								WHERE
									s.webhook_endpoint_id = e.id
									AND (
										s.state = 'ACTIVE'
										OR (
											s.state = 'RETIRING'
											AND s.valid_until >
											    clock_timestamp()
										)
									)
								ORDER BY
									s.state,
									s.activated_at,
									s.id
							),
							ARRAY(
								SELECT
									s.encryption_key_version
								FROM partner_webhook_signing_secrets s
								WHERE
									s.webhook_endpoint_id = e.id
									AND (
										s.state = 'ACTIVE'
										OR (
											s.state = 'RETIRING'
											AND s.valid_until >
											    clock_timestamp()
										)
									)
								ORDER BY
									s.state,
									s.activated_at,
									s.id
							)
						FROM webhook_deliveries d
						JOIN partner_webhook_endpoints e
						  ON e.id =
						     d.webhook_endpoint_id
						JOIN outbox_events o
						  ON o.id =
						     d.outbox_event_id
						WHERE
							d.state = 'PENDING'
							AND (
								d.next_attempt_at IS NULL
								OR d.next_attempt_at <=
								   clock_timestamp()
							)
							AND (
								d.lease_until IS NULL
								OR d.lease_until <
								   clock_timestamp()
							)
						ORDER BY
							d.next_attempt_at
								NULLS FIRST,
							d.id
						FOR UPDATE OF d
						SKIP LOCKED
						LIMIT 1
					`,
				).Scan(
					&result.ID,
					&result.URL,
					&endpointState,
					&result.FactID,
					&result.EventID,
					&result.FactType,
					&result.AggregateType,
					&result.AggregateID,
					&result.Payload,
					&result.OccurredAt,
					&result.Attempt,
					&ciphertexts,
					&encryptionKeyVersions,
				)

				if errors.Is(
					err,
					pgx.ErrNoRows,
				) {
					found = false
					return nil
				}

				if err != nil {
					return err
				}

				if endpointState != "ACTIVE" {
					if _, err = tx.Exec(
						ctx,
						`
							UPDATE webhook_deliveries
							SET
								state = 'CANCELLED',
								next_attempt_at = NULL,
								leased_by = NULL,
								lease_until = NULL,
								last_error =
								    'endpoint disabled'
							WHERE id = $1
							  AND state = 'PENDING'
						`,
						result.ID,
					); err != nil {
						return err
					}

					continue
				}

				if len(ciphertexts) !=
					len(encryptionKeyVersions) {
					return errors.New(
						"webhook signing secret key-version metadata is inconsistent",
					)
				}

				if len(ciphertexts) == 0 {
					return errors.New(
						"webhook endpoint has no usable signing secret",
					)
				}

				result.Secrets = nil

				for index, ciphertext := range ciphertexts {
					secret, openErr :=
						w.box.OpenVersion(
							int(encryptionKeyVersions[index]),
							ciphertext,
						)
					if openErr != nil {
						return openErr
					}

					result.Secrets =
						append(
							result.Secrets,
							secret,
						)
				}

				result.Attempt++

				commandTag, err :=
					tx.Exec(
						ctx,
						`
							UPDATE webhook_deliveries
							SET
								attempt_count = $2,
								leased_by = $3,
								lease_until =
								    clock_timestamp() +
								    $4::interval
							WHERE id = $1
							  AND state = 'PENDING'
						`,
						result.ID,
						result.Attempt,
						w.workerID,
						fmt.Sprintf(
							"%f seconds",
							w.lease.Seconds(),
						),
					)
				if err != nil {
					return err
				}

				if commandTag.RowsAffected() != 1 {
					return errors.New(
						"webhook delivery changed during lease claim",
					)
				}

				found = true

				return nil
			}
		},
	)

	return result, found, err
}

func (w *DeliveryWorker) deliver(ctx context.Context, d claimedDelivery) error {
	body, err := deliveryBody(d)
	if err != nil {
		return w.finish(ctx, d, 0, "ENCODING_ERROR", err.Error(), 0)
	}
	timestamp := time.Now().Unix()
	signatures := make([]string, 0, len(d.Secrets))
	signed := append([]byte(strconv.FormatInt(timestamp, 10)+"."), body...)
	for _, secret := range d.Secrets {
		mac := hmac.New(sha256.New, secret)
		_, _ = mac.Write(signed)
		signatures = append(signatures, "v1="+hex.EncodeToString(mac.Sum(nil)))
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, d.URL, bytes.NewReader(body))
	if err != nil {
		return w.finish(ctx, d, 0, "REQUEST_ERROR", err.Error(), 0)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "TktSync-Webhooks/1")
	request.Header.Set("TktSync-Event-Id", publicid.Encode(publicid.EventFact, d.FactID))
	request.Header.Set("TktSync-Delivery-Id", publicid.Encode(publicid.WebhookDelivery, d.ID))
	request.Header.Set("TktSync-Signature", fmt.Sprintf("t=%d,%s", timestamp, strings.Join(signatures, ",")))
	started := time.Now()
	response, err := w.client.Do(request)
	duration := time.Since(started)
	if err != nil {
		return w.finish(ctx, d, 0, "NETWORK_ERROR", err.Error(), duration)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1024))
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return w.finish(ctx, d, response.StatusCode, "", "", duration)
	}
	return w.finish(ctx, d, response.StatusCode, "HTTP_ERROR", fmt.Sprintf("HTTP %d", response.StatusCode), duration)
}

func deliveryBody(d claimedDelivery) ([]byte, error) {
	data := map[string]any{}
	if len(d.Payload) > 0 {
		_ = json.Unmarshal(d.Payload, &data)
	}
	if d.AggregateID != nil {
		data["aggregate_id"] = publicAggregateID(d.AggregateType, *d.AggregateID)
	}
	envelope := map[string]any{"id": publicid.Encode(publicid.EventFact, d.FactID), "type": d.FactType, "schema_version": 1, "occurred_at": d.OccurredAt, "data": data}
	if d.EventID != nil {
		envelope["event_id"] = publicid.Encode(publicid.Event, *d.EventID)
	}
	return json.Marshal(envelope)
}
func publicAggregateID(kind string, id uuid.UUID) string {
	switch kind {
	case "RESERVATION":
		return publicid.Encode(publicid.Reservation, id)
	case "TICKET":
		return publicid.Encode(publicid.Ticket, id)
	case "ADMISSION":
		return publicid.Encode(publicid.Admission, id)
	case "EVENT":
		return publicid.Encode(publicid.Event, id)
	case "PARTNER":
		return publicid.Encode(publicid.Partner, id)
	default:
		return id.String()
	}
}

func (w *DeliveryWorker) finish(
	ctx context.Context,
	d claimedDelivery,
	status int,
	errorClass string,
	message string,
	duration time.Duration,
) error {
	return w.transactions.Run(
		ctx,
		func(tx pgx.Tx) error {
			delivered :=
				status >= 200 &&
					status < 300

			state := "PENDING"
			retryInterval := "0 seconds"

			if delivered {
				state = "DELIVERED"
			} else if d.Attempt >=
				w.maxAttempts {
				state = "DEAD_LETTER"
			} else {
				retryInterval =
					fmt.Sprintf(
						"%f seconds",
						deliveryRetryDelay(
							d.Attempt,
						).Seconds(),
					)
			}

			if _, err := tx.Exec(
				ctx,
				`
					INSERT INTO webhook_delivery_attempts (
						id,
						webhook_delivery_id,
						attempt_number,
						attempted_at,
						duration_ms,
						status_code,
						error_class,
						response_excerpt
					)
					VALUES (
						$1,
						$2,
						$3,
						clock_timestamp(),
						$4,
						NULLIF($5, 0),
						NULLIF($6, ''),
						NULLIF($7, '')
					)
					ON CONFLICT (
						webhook_delivery_id,
						attempt_number
					)
					DO NOTHING
				`,
				uuid.New(),
				d.ID,
				d.Attempt,
				duration.Milliseconds(),
				status,
				errorClass,
				bounded(
					message,
					512,
				),
			); err != nil {
				return err
			}

			_, err := tx.Exec(
				ctx,
				`
					UPDATE webhook_deliveries
					SET
						state = $2,
						next_attempt_at =
							CASE
								WHEN $2 = 'PENDING'
								THEN
									clock_timestamp() +
									$3::interval
								ELSE NULL
							END,
						last_status_code =
							NULLIF($4, 0),
						last_error =
							NULLIF($5, ''),
						delivered_at =
							CASE
								WHEN $2 = 'DELIVERED'
								THEN clock_timestamp()
								ELSE delivered_at
							END,
						dead_lettered_at =
							CASE
								WHEN $2 = 'DEAD_LETTER'
								THEN clock_timestamp()
								ELSE dead_lettered_at
							END,
						leased_by = NULL,
						lease_until = NULL
					WHERE id = $1
					  AND state = 'PENDING'
					  AND leased_by = $6
					  AND attempt_count = $7
				`,
				d.ID,
				state,
				retryInterval,
				status,
				bounded(
					message,
					512,
				),
				w.workerID,
				d.Attempt,
			)

			return err
		},
	)
}

func deliveryRetryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	shift := attempt - 1
	if shift > 8 {
		shift = 8
	}
	d := time.Second * time.Duration(1<<shift)
	if d > 5*time.Minute {
		return 5 * time.Minute
	}
	return d
}
func bounded(value string, n int) string {
	value = strings.Map(func(r rune) rune {
		if r < ' ' && r != '\t' {
			return -1
		}
		return r
	}, value)
	if len(value) > n {
		return value[:n]
	}
	return value
}
