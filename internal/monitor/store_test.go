package monitor

import (
	"testing"
	"time"
)

func TestStoreBoundsHistoryAndPublishesUpdates(t *testing.T) {
	target := Target{ID: "api", Name: "API", URL: "https://example.com"}
	store := NewStore([]Target{target}, 2)
	updates, unsubscribe := store.Subscribe()
	defer unsubscribe()

	for index := 1; index <= 3; index++ {
		store.Record(Result{
			TargetID:   target.ID,
			CheckedAt:  time.Unix(int64(index), 0).UTC(),
			Latency:    time.Duration(index) * time.Millisecond,
			StatusCode: 200,
			Healthy:    index != 2,
		})
	}

	detail, ok := store.Get(target.ID)
	if !ok {
		t.Fatal("Get() did not find target")
	}
	if detail.Checks != 3 || detail.Failures != 1 || len(detail.History) != 2 {
		t.Fatalf("unexpected detail: %+v", detail)
	}
	if detail.History[0].CheckedAt.Unix() != 2 || detail.History[1].CheckedAt.Unix() != 3 {
		t.Fatalf("unexpected bounded history: %+v", detail.History)
	}

	for index := 0; index < 3; index++ {
		select {
		case update := <-updates:
			if update.ID != target.ID {
				t.Fatalf("unexpected update: %+v", update)
			}
		default:
			t.Fatalf("missing update %d", index+1)
		}
	}
}

func TestStoreListsPendingTargetsInStableOrder(t *testing.T) {
	store := NewStore([]Target{
		{ID: "zeta", Name: "Zeta"},
		{ID: "alpha", Name: "Alpha"},
	}, 1)

	monitors := store.List()
	if len(monitors) != 2 || monitors[0].ID != "alpha" || monitors[1].ID != "zeta" {
		t.Fatalf("unexpected order: %+v", monitors)
	}
	if !monitors[0].Pending || monitors[0].CheckedAt != nil {
		t.Fatalf("unexpected initial state: %+v", monitors[0])
	}
}

func TestStoreDisconnectsSlowSubscriber(t *testing.T) {
	store := NewStore([]Target{{ID: "api"}}, 1)
	updates, unsubscribe := store.Subscribe()
	defer unsubscribe()

	for index := 0; index <= subscriberBufferSize; index++ {
		store.Record(Result{TargetID: "api", CheckedAt: time.Now().UTC(), Healthy: true})
	}

	received := 0
	deadline := time.After(time.Second)
	for {
		select {
		case _, open := <-updates:
			if !open {
				if received != subscriberBufferSize {
					t.Fatalf("received %d buffered updates, want %d", received, subscriberBufferSize)
				}
				return
			}
			received++
		case <-deadline:
			t.Fatal("slow subscriber was not disconnected")
		}
	}
}
