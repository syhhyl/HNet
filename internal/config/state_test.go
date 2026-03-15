package config

import "testing"

func TestPersistedStateNormalizeSubscriptionsMigratesLegacyField(t *testing.T) {
	state := PersistedState{
		SubscriptionURL: "https://example.com/sub?token=abc",
	}

	state.normalizeSubscriptions()

	if len(state.Subscriptions) != 1 {
		t.Fatalf("expected 1 subscription after migration, got %d", len(state.Subscriptions))
	}
	if state.Subscriptions[0].URL != state.SubscriptionURL {
		t.Fatalf("expected migrated subscription %q, got %q", state.SubscriptionURL, state.Subscriptions[0].URL)
	}
	if state.Subscriptions[0].Name == "" {
		t.Fatal("expected migrated subscription to get a generated name")
	}
}

func TestPersistedStateUpsertSubscriptionDeduplicates(t *testing.T) {
	state := PersistedState{}
	state.UpsertSubscription("https://a.example.com/sub")
	state.UpsertSubscription("https://a.example.com/sub")
	state.UpsertSubscription("https://b.example.com/sub")

	if len(state.Subscriptions) != 2 {
		t.Fatalf("expected 2 subscriptions, got %d", len(state.Subscriptions))
	}
	if state.SubscriptionURL != "https://b.example.com/sub" {
		t.Fatalf("expected current subscription to be the latest one, got %q", state.SubscriptionURL)
	}
	if state.Subscriptions[0].Name != "a.example.com" {
		t.Fatalf("expected generated name a.example.com, got %q", state.Subscriptions[0].Name)
	}
	if state.Subscriptions[1].Name != "b.example.com" {
		t.Fatalf("expected generated name b.example.com, got %q", state.Subscriptions[1].Name)
	}
}

func TestPersistedStateSelectSubscription(t *testing.T) {
	state := PersistedState{
		Subscriptions: []SubscriptionEntry{{URL: "https://a.example.com/sub"}, {URL: "https://b.example.com/sub"}},
	}
	state.normalizeSubscriptions()

	if !state.SelectSubscription("https://b.example.com/sub") {
		t.Fatal("expected SelectSubscription to succeed")
	}
	if state.SubscriptionURL != "https://b.example.com/sub" {
		t.Fatalf("expected current subscription to be updated, got %q", state.SubscriptionURL)
	}
	if state.SelectSubscription("https://missing.example.com/sub") {
		t.Fatal("expected SelectSubscription to fail for missing subscription")
	}
}

func TestPersistedStateDeleteInactiveSubscription(t *testing.T) {
	state := PersistedState{
		SubscriptionURL: "https://a.example.com/sub",
		Subscriptions: []SubscriptionEntry{
			{URL: "https://a.example.com/sub"},
			{URL: "https://b.example.com/sub"},
		},
	}
	state.normalizeSubscriptions()

	removed, deletedActive, nextURL := state.DeleteSubscription("https://b.example.com/sub")
	if !removed {
		t.Fatal("expected delete to succeed")
	}
	if deletedActive {
		t.Fatal("expected deleted subscription to be inactive")
	}
	if nextURL != "https://a.example.com/sub" {
		t.Fatalf("expected active subscription to stay on a.example.com, got %q", nextURL)
	}
	if len(state.Subscriptions) != 1 {
		t.Fatalf("expected 1 subscription after delete, got %d", len(state.Subscriptions))
	}
}

func TestPersistedStateDeleteActiveSubscriptionSelectsNeighbor(t *testing.T) {
	state := PersistedState{
		SubscriptionURL: "https://b.example.com/sub",
		Subscriptions: []SubscriptionEntry{
			{URL: "https://a.example.com/sub"},
			{URL: "https://b.example.com/sub"},
			{URL: "https://c.example.com/sub"},
		},
	}
	state.normalizeSubscriptions()

	removed, deletedActive, nextURL := state.DeleteSubscription("https://b.example.com/sub")
	if !removed {
		t.Fatal("expected delete to succeed")
	}
	if !deletedActive {
		t.Fatal("expected deleted subscription to be active")
	}
	if nextURL != "https://c.example.com/sub" {
		t.Fatalf("expected next subscription to be c.example.com, got %q", nextURL)
	}
}

func TestPersistedStateDeleteLastSubscriptionClearsActive(t *testing.T) {
	state := PersistedState{
		SubscriptionURL: "https://a.example.com/sub",
		Subscriptions:   []SubscriptionEntry{{URL: "https://a.example.com/sub"}},
	}
	state.normalizeSubscriptions()

	removed, deletedActive, nextURL := state.DeleteSubscription("https://a.example.com/sub")
	if !removed || !deletedActive {
		t.Fatalf("expected active subscription to be deleted, got removed=%t deletedActive=%t", removed, deletedActive)
	}
	if nextURL != "" {
		t.Fatalf("expected no next subscription, got %q", nextURL)
	}
	if state.SubscriptionURL != "" {
		t.Fatalf("expected active subscription to be cleared, got %q", state.SubscriptionURL)
	}
	if len(state.Subscriptions) != 0 {
		t.Fatalf("expected no subscriptions left, got %d", len(state.Subscriptions))
	}
}
