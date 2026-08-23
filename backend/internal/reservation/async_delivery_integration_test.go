//go:build integration

package reservation_test

import (
	"bufio"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tktsync/tktsync/backend/internal/auth"
	"github.com/tktsync/tktsync/backend/internal/outbox"
	"github.com/tktsync/tktsync/backend/internal/platform/publicid"
	"github.com/tktsync/tktsync/backend/internal/realtimeapi"
	"github.com/tktsync/tktsync/backend/internal/webhook"
)

func TestAsyncDeliverySSEEmitsOnlyProcessedCommittedFacts(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	handler := realtimeapi.New(f.pool, func(context.Context, string) (auth.HumanPrincipal, error) {
		return auth.HumanPrincipal{Provider: "reservation", Subject: f.userSubject}, nil
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/v1/realtime/stream?audience=admin&event_id="+publicid.Encode(publicid.Event, f.eventID), nil)
	request.Header.Set("Authorization", "Bearer human-session")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("SSE status=%d", response.StatusCode)
	}
	scanner := bufio.NewScanner(response.Body)
	for scanner.Scan() {
		if scanner.Text() == "event: resync" {
			break
		}
	}
	factID := uuid.New()
	if _, err = f.pool.Exec(ctx, `INSERT INTO outbox_events(id,fact_id,event_id,fact_type,aggregate_type,aggregate_id,payload,created_at,processed_at) VALUES($1,$2,$3,'event.configuration_updated','EVENT',$3,'{}',clock_timestamp(),clock_timestamp())`, uuid.New(), factID, f.eventID); err != nil {
		t.Fatal(err)
	}
	found := false
	for scanner.Scan() {
		if scanner.Text() == "event: invalidate" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("SSE did not emit processed committed fact: %v", scanner.Err())
	}
}

func TestAsyncDeliveryRolledBackFactIsNotObservable(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	factID := uuid.New()
	if _, err = (outbox.Store{}).Append(ctx, tx, outbox.Fact{
		FactID:        factID,
		EventID:       &f.eventID,
		FactType:      "event.configuration_updated",
		AggregateType: "EVENT",
		AggregateID:   &f.eventID,
	}); err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	var count int
	if err = f.pool.QueryRow(ctx, `SELECT COUNT(*) FROM outbox_events WHERE fact_id=$1`, factID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("rolled-back fact persisted: count=%d", count)
	}
}

func TestAsyncDeliveryOutboxWebhookRetrySigningAndConcurrentDispatch(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if _, err := f.pool.Exec(ctx, `UPDATE outbox_events SET processed_at=clock_timestamp() WHERE processed_at IS NULL`); err != nil {
		t.Fatal(err)
	}
	box := asyncDeliverySecretBox(t)
	var requests atomic.Int32
	var receivedBody []byte
	var receivedSignature, eventHeader, deliveryHeader string
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		receivedBody = body
		receivedSignature = r.Header.Get("TktSync-Signature")
		eventHeader = r.Header.Get("TktSync-Event-Id")
		deliveryHeader = r.Header.Get("TktSync-Delivery-Id")
		mu.Unlock()
		if requests.Add(1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	service := webhook.NewService(f.runner, box, 1, true)
	endpoint, err := service.CreateEndpoint(ctx, f.userID, f.partnerID, server.URL, []string{"reservation.confirmed"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(mustSecretCiphertext(t, ctx, f, endpoint.ID)), endpoint.Secret) {
		t.Fatal("plaintext webhook secret persisted")
	}
	_, _ = confirmedAdmissionTicket(t, ctx, f, "A1")
	dispatcherA, dispatcherB := outbox.NewDispatcher(f.runner, 100), outbox.NewDispatcher(f.runner, 100)
	var wg sync.WaitGroup
	wg.Add(2)
	errs := make([]error, 2)
	go func() { defer wg.Done(); errs[0] = dispatcherA.RunOnce(ctx) }()
	go func() { defer wg.Done(); errs[1] = dispatcherB.RunOnce(ctx) }()
	wg.Wait()
	for _, dispatchErr := range errs {
		if dispatchErr != nil {
			t.Fatal(dispatchErr)
		}
	}
	var deliveryID uuid.UUID
	var deliveries int
	if err = f.pool.QueryRow(ctx, `SELECT COUNT(*) FROM webhook_deliveries WHERE webhook_endpoint_id=$1`, endpoint.ID).Scan(&deliveries); err != nil {
		t.Fatal(err)
	}
	if deliveries != 1 {
		t.Fatalf("deliveries=%d want=1", deliveries)
	}
	if err = f.pool.QueryRow(ctx, `SELECT id FROM webhook_deliveries WHERE webhook_endpoint_id=$1`, endpoint.ID).Scan(&deliveryID); err != nil {
		t.Fatal(err)
	}
	worker := webhook.NewDeliveryWorker(f.runner, box, true, 10, 4, 2*time.Second)
	if err = webhook.NewDeliveryWorker(f.runner, box, true, 10, 4, 2*time.Second).RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 {
		t.Fatalf("attempts=%d want=1", requests.Load())
	}
	if _, err = f.pool.Exec(ctx, `UPDATE webhook_deliveries SET next_attempt_at=clock_timestamp() WHERE id=$1`, deliveryID); err != nil {
		t.Fatal(err)
	}
	if err = worker.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 {
		t.Fatalf("attempts=%d want=2", requests.Load())
	}
	mu.Lock()
	body, signature, eventID, deliveryPublicID := append([]byte(nil), receivedBody...), receivedSignature, eventHeader, deliveryHeader
	mu.Unlock()
	if eventID == "" || deliveryPublicID == "" || eventID == deliveryPublicID {
		t.Fatalf("invalid stable delivery headers event=%q delivery=%q", eventID, deliveryPublicID)
	}
	verifyAsyncDeliverySignature(t, endpoint.Secret, signature, body)
	if strings.Contains(string(body), "qr1.") || strings.Contains(string(body), endpoint.Secret) {
		t.Fatal("webhook leaked secret material")
	}
	var state string
	var attempts int
	if err = f.pool.QueryRow(ctx, `SELECT state,attempt_count FROM webhook_deliveries WHERE id=$1`, deliveryID).Scan(&state, &attempts); err != nil {
		t.Fatal(err)
	}
	if state != "DELIVERED" || attempts != 2 {
		t.Fatalf("state/attempts=%s/%d", state, attempts)
	}
	rotated, _, err := service.RotateSecret(ctx, f.userID, endpoint.ID)
	if err != nil || rotated == endpoint.Secret {
		t.Fatalf("secret rotation failed: %v", err)
	}
	if err = service.ReplaceSubscriptions(ctx, f.userID, endpoint.ID, []string{"ticket.voided", "reservation.confirmed"}); err != nil {
		t.Fatal(err)
	}
	var activeSecrets, retiringSecrets int
	if err = f.pool.QueryRow(ctx, `SELECT COUNT(*) FILTER(WHERE state='ACTIVE'),COUNT(*) FILTER(WHERE state='RETIRING') FROM partner_webhook_signing_secrets WHERE webhook_endpoint_id=$1`, endpoint.ID).Scan(&activeSecrets, &retiringSecrets); err != nil {
		t.Fatal(err)
	}
	if activeSecrets != 1 || retiringSecrets != 1 {
		t.Fatalf("secret states active/retiring=%d/%d", activeSecrets, retiringSecrets)
	}
}

func TestAsyncDeliveryDisabledAndUnsubscribedEndpointsReceiveNothing(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if _, err := f.pool.Exec(ctx, `UPDATE outbox_events SET processed_at=clock_timestamp() WHERE processed_at IS NULL`); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("disabled endpoint called") }))
	defer server.Close()
	service := webhook.NewService(f.runner, asyncDeliverySecretBox(t), 1, true)
	endpoint, err := service.CreateEndpoint(ctx, f.userID, f.partnerID, server.URL, []string{"ticket.voided"})
	if err != nil {
		t.Fatal(err)
	}
	if err = service.DisableEndpoint(ctx, f.userID, endpoint.ID, "operator disabled"); err != nil {
		t.Fatal(err)
	}
	_, _ = confirmedAdmissionTicket(t, ctx, f, "A2")
	if err = outbox.NewDispatcher(f.runner, 100).RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = f.pool.QueryRow(ctx, `SELECT COUNT(*) FROM webhook_deliveries WHERE webhook_endpoint_id=$1`, endpoint.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("disabled endpoint deliveries=%d", count)
	}
}

func asyncDeliverySecretBox(t *testing.T) *webhook.SecretBox {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(200 - i)
	}
	box, err := webhook.NewSecretBox(base64.RawURLEncoding.EncodeToString(key))
	if err != nil {
		t.Fatal(err)
	}
	return box
}
func mustSecretCiphertext(t *testing.T, ctx context.Context, f fixture, endpointID uuid.UUID) []byte {
	t.Helper()
	var value []byte
	if err := f.pool.QueryRow(ctx, `SELECT secret_ciphertext FROM partner_webhook_signing_secrets WHERE webhook_endpoint_id=$1 AND state='ACTIVE'`, endpointID).Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}
func verifyAsyncDeliverySignature(t *testing.T, secret, header string, body []byte) {
	t.Helper()
	parts := strings.Split(header, ",")
	if len(parts) < 2 || !strings.HasPrefix(parts[0], "t=") || !strings.HasPrefix(parts[1], "v1=") {
		t.Fatalf("signature=%q", header)
	}
	rawSecret, err := base64.RawURLEncoding.DecodeString(secret)
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, rawSecret)
	_, _ = mac.Write(append([]byte(strings.TrimPrefix(parts[0], "t=")+"."), body...))
	if hex.EncodeToString(mac.Sum(nil)) != strings.TrimPrefix(parts[1], "v1=") {
		t.Fatal("webhook HMAC mismatch")
	}
}

func TestAsyncDeliveryDisabledPendingDeliveryDoesNotBlockActiveDelivery(
	t *testing.T,
) {
	f := newFixture(t)

	ctx, cancel :=
		context.WithTimeout(
			context.Background(),
			60*time.Second,
		)
	defer cancel()

	if _, err := f.pool.Exec(
		ctx,
		`
			UPDATE webhook_deliveries
			SET
				state = 'CANCELLED',
				next_attempt_at = NULL,
				leased_by = NULL,
				lease_until = NULL
			WHERE state = 'PENDING'
		`,
	); err != nil {
		t.Fatal(err)
	}

	if _, err := f.pool.Exec(
		ctx,
		`
			UPDATE outbox_events
			SET processed_at =
			    clock_timestamp()
			WHERE processed_at IS NULL
		`,
	); err != nil {
		t.Fatal(err)
	}

	box := asyncDeliverySecretBox(t)

	disabledServer :=
		httptest.NewServer(
			http.HandlerFunc(
				func(
					http.ResponseWriter,
					*http.Request,
				) {
					t.Error(
						"disabled endpoint was called",
					)
				},
			),
		)
	defer disabledServer.Close()

	var activeCalls atomic.Int32

	activeServer :=
		httptest.NewServer(
			http.HandlerFunc(
				func(
					w http.ResponseWriter,
					_ *http.Request,
				) {
					activeCalls.Add(1)
					w.WriteHeader(
						http.StatusNoContent,
					)
				},
			),
		)
	defer activeServer.Close()

	service :=
		webhook.NewService(
			f.runner,
			box,
			1,
			true,
		)

	disabledEndpoint, err :=
		service.CreateEndpoint(
			ctx,
			f.userID,
			f.partnerID,
			disabledServer.URL,
			[]string{
				"reservation.confirmed",
			},
		)
	if err != nil {
		t.Fatal(err)
	}

	activeEndpoint, err :=
		service.CreateEndpoint(
			ctx,
			f.userID,
			f.partnerID,
			activeServer.URL,
			[]string{
				"reservation.confirmed",
			},
		)
	if err != nil {
		t.Fatal(err)
	}

	_, _ =
		confirmedAdmissionTicket(
			t,
			ctx,
			f,
			"A3",
		)

	if err =
		outbox.NewDispatcher(
			f.runner,
			100,
		).RunOnce(ctx); err != nil {
		t.Fatal(err)
	}

	var (
		disabledDeliveryID uuid.UUID
		activeDeliveryID   uuid.UUID
	)

	if err = f.pool.QueryRow(
		ctx,
		`
			SELECT id
			FROM webhook_deliveries
			WHERE webhook_endpoint_id = $1
			  AND state = 'PENDING'
			ORDER BY created_at DESC
			LIMIT 1
		`,
		disabledEndpoint.ID,
	).Scan(
		&disabledDeliveryID,
	); err != nil {
		t.Fatal(err)
	}

	if err = f.pool.QueryRow(
		ctx,
		`
			SELECT id
			FROM webhook_deliveries
			WHERE webhook_endpoint_id = $1
			  AND state = 'PENDING'
			ORDER BY created_at DESC
			LIMIT 1
		`,
		activeEndpoint.ID,
	).Scan(
		&activeDeliveryID,
	); err != nil {
		t.Fatal(err)
	}

	if err =
		service.DisableEndpoint(
			ctx,
			f.userID,
			disabledEndpoint.ID,
			"disabled after enqueue",
		); err != nil {
		t.Fatal(err)
	}

	if _, err = f.pool.Exec(
		ctx,
		`
			UPDATE webhook_deliveries
			SET next_attempt_at =
				CASE
					WHEN id = $1
					THEN
						clock_timestamp() -
						interval '2 minutes'
					WHEN id = $2
					THEN
						clock_timestamp() -
						interval '1 minute'
					ELSE next_attempt_at
				END
			WHERE id IN ($1, $2)
		`,
		disabledDeliveryID,
		activeDeliveryID,
	); err != nil {
		t.Fatal(err)
	}

	worker :=
		webhook.NewDeliveryWorker(
			f.runner,
			box,
			true,
			10,
			4,
			2*time.Second,
		)

	if err = worker.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}

	if activeCalls.Load() != 1 {
		t.Fatalf(
			"active endpoint calls=%d want=1",
			activeCalls.Load(),
		)
	}

	var (
		disabledState string
		activeState   string
	)

	if err = f.pool.QueryRow(
		ctx,
		`
			SELECT state
			FROM webhook_deliveries
			WHERE id = $1
		`,
		disabledDeliveryID,
	).Scan(
		&disabledState,
	); err != nil {
		t.Fatal(err)
	}

	if err = f.pool.QueryRow(
		ctx,
		`
			SELECT state
			FROM webhook_deliveries
			WHERE id = $1
		`,
		activeDeliveryID,
	).Scan(
		&activeState,
	); err != nil {
		t.Fatal(err)
	}

	if disabledState != "CANCELLED" ||
		activeState != "DELIVERED" {
		t.Fatalf(
			"delivery states disabled=%s active=%s want=CANCELLED/DELIVERED",
			disabledState,
			activeState,
		)
	}
}

func TestAsyncDeliveryUsesPersistedEncryptionKeyVersion(
	t *testing.T,
) {
	f := newFixture(t)

	ctx, cancel :=
		context.WithTimeout(
			context.Background(),
			60*time.Second,
		)
	defer cancel()

	if _, err := f.pool.Exec(
		ctx,
		`
			UPDATE webhook_deliveries
			SET
				state = 'CANCELLED',
				next_attempt_at = NULL,
				leased_by = NULL,
				lease_until = NULL
			WHERE state = 'PENDING'
		`,
	); err != nil {
		t.Fatal(err)
	}

	if _, err := f.pool.Exec(
		ctx,
		`
			UPDATE outbox_events
			SET processed_at =
			    clock_timestamp()
			WHERE processed_at IS NULL
		`,
	); err != nil {
		t.Fatal(err)
	}

	oldRaw := make([]byte, 32)
	newRaw := make([]byte, 32)

	for index := range oldRaw {
		oldRaw[index] =
			byte(index + 11)
		newRaw[index] =
			byte(230 - index)
	}

	oldEncoded :=
		base64.RawURLEncoding.EncodeToString(
			oldRaw,
		)

	newEncoded :=
		base64.RawURLEncoding.EncodeToString(
			newRaw,
		)

	oldBox, err :=
		webhook.NewSecretBox(
			oldEncoded,
		)
	if err != nil {
		t.Fatal(err)
	}

	service :=
		webhook.NewService(
			f.runner,
			oldBox,
			1,
			true,
		)

	var calls atomic.Int32

	server :=
		httptest.NewServer(
			http.HandlerFunc(
				func(
					w http.ResponseWriter,
					_ *http.Request,
				) {
					calls.Add(1)
					w.WriteHeader(
						http.StatusNoContent,
					)
				},
			),
		)
	defer server.Close()

	endpoint, err :=
		service.CreateEndpoint(
			ctx,
			f.userID,
			f.partnerID,
			server.URL,
			[]string{
				"reservation.confirmed",
			},
		)
	if err != nil {
		t.Fatal(err)
	}

	var persistedVersion int

	if err = f.pool.QueryRow(
		ctx,
		`
			SELECT encryption_key_version
			FROM partner_webhook_signing_secrets
			WHERE webhook_endpoint_id = $1
			  AND state = 'ACTIVE'
		`,
		endpoint.ID,
	).Scan(
		&persistedVersion,
	); err != nil {
		t.Fatal(err)
	}

	if persistedVersion != 1 {
		t.Fatalf(
			"persisted encryption version=%d want=1",
			persistedVersion,
		)
	}

	_, _ =
		confirmedAdmissionTicket(
			t,
			ctx,
			f,
			"A4",
		)

	if err =
		outbox.NewDispatcher(
			f.runner,
			100,
		).RunOnce(ctx); err != nil {
		t.Fatal(err)
	}

	ring, err :=
		webhook.NewVersionedSecretBox(
			2,
			newEncoded,
			`{"1":"`+
				oldEncoded+
				`"}`,
		)
	if err != nil {
		t.Fatal(err)
	}

	worker :=
		webhook.NewDeliveryWorker(
			f.runner,
			ring,
			true,
			10,
			4,
			2*time.Second,
		)

	if err = worker.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}

	if calls.Load() != 1 {
		t.Fatalf(
			"historical-key delivery calls=%d want=1",
			calls.Load(),
		)
	}

	var state string

	if err = f.pool.QueryRow(
		ctx,
		`
			SELECT d.state
			FROM webhook_deliveries d
			WHERE d.webhook_endpoint_id = $1
			ORDER BY d.created_at DESC
			LIMIT 1
		`,
		endpoint.ID,
	).Scan(
		&state,
	); err != nil {
		t.Fatal(err)
	}

	if state != "DELIVERED" {
		t.Fatalf(
			"historical-key delivery state=%s want=DELIVERED",
			state,
		)
	}
}

func TestAsyncDeliveryExpiredLeaseStaleAttemptCannotOverwriteReclaim(
	t *testing.T,
) {
	f := newFixture(t)

	ctx, cancel :=
		context.WithTimeout(
			context.Background(),
			60*time.Second,
		)
	defer cancel()

	if _, err := f.pool.Exec(
		ctx,
		`
			UPDATE webhook_deliveries
			SET
				state = 'CANCELLED',
				next_attempt_at = NULL,
				leased_by = NULL,
				lease_until = NULL
			WHERE state = 'PENDING'
		`,
	); err != nil {
		t.Fatal(err)
	}

	if _, err := f.pool.Exec(
		ctx,
		`
			UPDATE outbox_events
			SET processed_at =
			    clock_timestamp()
			WHERE processed_at IS NULL
		`,
	); err != nil {
		t.Fatal(err)
	}

	box := asyncDeliverySecretBox(t)

	firstStarted :=
		make(chan struct{})
	secondStarted :=
		make(chan struct{})
	releaseFirst :=
		make(chan struct{})
	releaseSecond :=
		make(chan struct{})

	var requests atomic.Int32

	server :=
		httptest.NewServer(
			http.HandlerFunc(
				func(
					w http.ResponseWriter,
					_ *http.Request,
				) {
					switch requests.Add(1) {
					case 1:
						close(firstStarted)
						<-releaseFirst
						w.WriteHeader(
							http.StatusInternalServerError,
						)

					case 2:
						close(secondStarted)
						<-releaseSecond
						w.WriteHeader(
							http.StatusNoContent,
						)

					default:
						w.WriteHeader(
							http.StatusNoContent,
						)
					}
				},
			),
		)
	defer server.Close()

	service :=
		webhook.NewService(
			f.runner,
			box,
			1,
			true,
		)

	endpoint, err :=
		service.CreateEndpoint(
			ctx,
			f.userID,
			f.partnerID,
			server.URL,
			[]string{
				"reservation.confirmed",
			},
		)
	if err != nil {
		t.Fatal(err)
	}

	_, _ =
		confirmedAdmissionTicket(
			t,
			ctx,
			f,
			"A5",
		)

	if err =
		outbox.NewDispatcher(
			f.runner,
			100,
		).RunOnce(ctx); err != nil {
		t.Fatal(err)
	}

	var deliveryID uuid.UUID

	if err = f.pool.QueryRow(
		ctx,
		`
			SELECT id
			FROM webhook_deliveries
			WHERE webhook_endpoint_id = $1
			  AND state = 'PENDING'
			ORDER BY created_at DESC
			LIMIT 1
		`,
		endpoint.ID,
	).Scan(
		&deliveryID,
	); err != nil {
		t.Fatal(err)
	}

	worker :=
		webhook.NewDeliveryWorker(
			f.runner,
			box,
			true,
			1,
			4,
			10*time.Second,
		)

	firstDone :=
		make(chan error, 1)
	secondDone :=
		make(chan error, 1)

	go func() {
		firstDone <- worker.RunOnce(ctx)
	}()

	select {
	case <-firstStarted:
	case <-time.After(
		5 * time.Second,
	):
		t.Fatal(
			"first delivery did not start",
		)
	}

	if _, err = f.pool.Exec(
		ctx,
		`
			UPDATE webhook_deliveries
			SET lease_until =
			    clock_timestamp() -
			    interval '1 second'
			WHERE id = $1
		`,
		deliveryID,
	); err != nil {
		t.Fatal(err)
	}

	go func() {
		secondDone <- worker.RunOnce(ctx)
	}()

	select {
	case <-secondStarted:
	case <-time.After(
		5 * time.Second,
	):
		t.Fatal(
			"reclaimed delivery did not start",
		)
	}

	close(releaseFirst)

	select {
	case err = <-firstDone:
		if err != nil {
			t.Fatal(err)
		}

	case <-time.After(
		5 * time.Second,
	):
		t.Fatal(
			"stale attempt did not finish",
		)
	}

	var (
		midState   string
		midAttempt int
	)

	if err = f.pool.QueryRow(
		ctx,
		`
			SELECT
				state,
				attempt_count
			FROM webhook_deliveries
			WHERE id = $1
		`,
		deliveryID,
	).Scan(
		&midState,
		&midAttempt,
	); err != nil {
		t.Fatal(err)
	}

	if midState != "PENDING" ||
		midAttempt != 2 {
		t.Fatalf(
			"stale attempt overwrote reclaimed delivery state=%s attempts=%d want=PENDING/2",
			midState,
			midAttempt,
		)
	}

	close(releaseSecond)

	select {
	case err = <-secondDone:
		if err != nil {
			t.Fatal(err)
		}

	case <-time.After(
		5 * time.Second,
	):
		t.Fatal(
			"reclaimed attempt did not finish",
		)
	}

	var finalState string

	if err = f.pool.QueryRow(
		ctx,
		`
			SELECT state
			FROM webhook_deliveries
			WHERE id = $1
		`,
		deliveryID,
	).Scan(
		&finalState,
	); err != nil {
		t.Fatal(err)
	}

	if finalState != "DELIVERED" {
		t.Fatalf(
			"final delivery state=%s want=DELIVERED",
			finalState,
		)
	}

	if requests.Load() != 2 {
		t.Fatalf(
			"network attempts=%d want=2",
			requests.Load(),
		)
	}
}
