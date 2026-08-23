package reservation_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tktsync/tktsync/backend/internal/auth"
	"github.com/tktsync/tktsync/backend/internal/platform/database"
	"github.com/tktsync/tktsync/backend/internal/reservation"
)

func TestDatabaseUnavailableFailsAuthoritativeMutationClosed(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), "postgres://tktsync:tktsync@127.0.0.1:1/tktsync?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	pool.Close()
	keys, err := auth.ParseHMACKeyring(1, "1:AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA")
	if err != nil {
		t.Fatal(err)
	}
	service := reservation.NewService(database.NewRunner(pool, 1, time.Millisecond), keys, keys)
	created, err := service.Create(context.Background(), reservation.CreateInput{
		EventID: uuid.New(), PartnerID: uuid.New(),
		Items: []reservation.ItemInput{{InventoryKind: reservation.InventoryReserved, InventoryID: uuid.New(), Quantity: 1, SourceKind: reservation.SourceShared}},
	})
	if err == nil || created.ReservationID != uuid.Nil {
		t.Fatalf("closed authority returned mutation success: created=%+v err=%v", created, err)
	}
}
