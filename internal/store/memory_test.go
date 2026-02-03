package store

import (
	"fmt"
	"testing"
	"time"

	"github.com/quotaguard/quotaguard/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMemoryStore(t *testing.T) {
	store := NewMemoryStore()
	require.NotNil(t, store)
	assert.NotNil(t, store.quotas)
	assert.NotNil(t, store.accounts)
	assert.NotNil(t, store.subscribers)
}

func TestMemoryStore_AccountOperations(t *testing.T) {
	store := NewMemoryStore()

	t.Run("Set and Get Account", func(t *testing.T) {
		acc := &models.Account{
			ID:       "acc-1",
			Provider: "openai",
			Tier:     "tier-1",
			Enabled:  true,
		}

		store.SetAccount(acc)

		got, ok := store.GetAccount("acc-1")
		require.True(t, ok)
		assert.Equal(t, acc.ID, got.ID)
		assert.Equal(t, acc.Provider, got.Provider)
	})

	t.Run("Get Non-existent Account", func(t *testing.T) {
		_, ok := store.GetAccount("non-existent")
		assert.False(t, ok)
	})

	t.Run("Delete Account", func(t *testing.T) {
		acc := &models.Account{
			ID:       "acc-to-delete",
			Provider: "anthropic",
		}
		store.SetAccount(acc)

		ok := store.DeleteAccount("acc-to-delete")
		assert.True(t, ok)

		_, ok = store.GetAccount("acc-to-delete")
		assert.False(t, ok)
	})

	t.Run("Delete Non-existent Account", func(t *testing.T) {
		ok := store.DeleteAccount("non-existent")
		assert.False(t, ok)
	})

	t.Run("List Accounts", func(t *testing.T) {
		store.Clear()

		acc1 := &models.Account{ID: "acc-1", Provider: "openai"}
		acc2 := &models.Account{ID: "acc-2", Provider: "anthropic"}

		store.SetAccount(acc1)
		store.SetAccount(acc2)

		accounts := store.ListAccounts()
		assert.Len(t, accounts, 2)
	})

	t.Run("List Enabled Accounts", func(t *testing.T) {
		store.Clear()

		acc1 := &models.Account{ID: "acc-1", Provider: "openai", Enabled: true}
		acc2 := &models.Account{ID: "acc-2", Provider: "anthropic", Enabled: false}
		acc3 := &models.Account{ID: "acc-3", Provider: "gemini", Enabled: true}

		store.SetAccount(acc1)
		store.SetAccount(acc2)
		store.SetAccount(acc3)

		enabled := store.ListEnabledAccounts()
		assert.Len(t, enabled, 2)
	})
}

func TestMemoryStore_QuotaOperations(t *testing.T) {
	store := NewMemoryStore()

	t.Run("Set and Get Quota", func(t *testing.T) {
		quota := &models.QuotaInfo{
			AccountID:             "acc-1",
			Provider:              "openai",
			EffectiveRemainingPct: 85.0,
		}

		store.SetQuota("acc-1", quota)

		got, ok := store.GetQuota("acc-1")
		require.True(t, ok)
		assert.Equal(t, quota.AccountID, got.AccountID)
		assert.Equal(t, quota.EffectiveRemainingPct, got.EffectiveRemainingPct)
	})

	t.Run("Get Non-existent Quota", func(t *testing.T) {
		_, ok := store.GetQuota("non-existent")
		assert.False(t, ok)
	})

	t.Run("Update Quota", func(t *testing.T) {
		oldQuota := &models.QuotaInfo{
			AccountID:             "acc-update",
			Provider:              "openai",
			EffectiveRemainingPct: 80.0,
		}
		store.SetQuota("acc-update", oldQuota)

		newQuota := &models.QuotaInfo{
			AccountID:             "acc-update",
			Provider:              "openai",
			EffectiveRemainingPct: 90.0,
		}

		err := store.UpdateQuota("acc-update", newQuota)
		require.NoError(t, err)

		got, _ := store.GetQuota("acc-update")
		assert.Equal(t, 90.0, got.EffectiveRemainingPct)
	})

	t.Run("Delete Quota", func(t *testing.T) {
		quota := &models.QuotaInfo{
			AccountID: "acc-to-delete",
			Provider:  "openai",
		}
		store.SetQuota("acc-to-delete", quota)

		ok := store.DeleteQuota("acc-to-delete")
		assert.True(t, ok)

		_, ok = store.GetQuota("acc-to-delete")
		assert.False(t, ok)
	})

	t.Run("Delete Non-existent Quota", func(t *testing.T) {
		ok := store.DeleteQuota("non-existent")
		assert.False(t, ok)
	})

	t.Run("List Quotas", func(t *testing.T) {
		store.Clear()

		quota1 := &models.QuotaInfo{AccountID: "acc-1", Provider: "openai"}
		quota2 := &models.QuotaInfo{AccountID: "acc-2", Provider: "anthropic"}

		store.SetQuota("acc-1", quota1)
		store.SetQuota("acc-2", quota2)

		quotas := store.ListQuotas()
		assert.Len(t, quotas, 2)
	})
}

func TestMemoryStore_Subscription(t *testing.T) {
	store := NewMemoryStore()

	t.Run("Subscribe and Receive Event", func(t *testing.T) {
		quota := &models.QuotaInfo{
			AccountID:             "acc-sub",
			Provider:              "openai",
			EffectiveRemainingPct: 80.0,
		}
		store.SetQuota("acc-sub", quota)

		ch := store.Subscribe("acc-sub")
		require.NotNil(t, ch)

		// Update quota to trigger event
		newQuota := &models.QuotaInfo{
			AccountID:             "acc-sub",
			Provider:              "openai",
			EffectiveRemainingPct: 90.0,
		}
		err := store.UpdateQuota("acc-sub", newQuota)
		require.NoError(t, err)

		select {
		case event := <-ch:
			assert.Equal(t, "acc-sub", event.AccountID)
			assert.Equal(t, int64(8000), event.OldValue) // 80.0 * 100
			assert.Equal(t, int64(9000), event.NewValue) // 90.0 * 100
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for event")
		}

		store.Unsubscribe("acc-sub", ch)
	})

	t.Run("Unsubscribe", func(t *testing.T) {
		quota := &models.QuotaInfo{
			AccountID: "acc-unsub",
			Provider:  "openai",
		}
		store.SetQuota("acc-unsub", quota)

		ch := store.Subscribe("acc-unsub")
		store.Unsubscribe("acc-unsub", ch)

		// Channel should be closed
		select {
		case _, ok := <-ch:
			assert.False(t, ok, "channel should be closed")
		case <-time.After(time.Second):
			// Channel might be closed already
		}
	})

	t.Run("Multiple Subscribers", func(t *testing.T) {
		quota := &models.QuotaInfo{
			AccountID:             "acc-multi-sub",
			Provider:              "openai",
			EffectiveRemainingPct: 70.0,
		}
		store.SetQuota("acc-multi-sub", quota)

		ch1 := store.Subscribe("acc-multi-sub")
		ch2 := store.Subscribe("acc-multi-sub")

		newQuota := &models.QuotaInfo{
			AccountID:             "acc-multi-sub",
			Provider:              "openai",
			EffectiveRemainingPct: 85.0,
		}
		err := store.UpdateQuota("acc-multi-sub", newQuota)
		require.NoError(t, err)

		// Both subscribers should receive the event
		select {
		case <-ch1:
			// OK
		case <-time.After(time.Second):
			t.Fatal("subscriber 1 timeout")
		}

		select {
		case <-ch2:
			// OK
		case <-time.After(time.Second):
			t.Fatal("subscriber 2 timeout")
		}

		store.Unsubscribe("acc-multi-sub", ch1)
		store.Unsubscribe("acc-multi-sub", ch2)
	})
}

func TestMemoryStore_Clear(t *testing.T) {
	store := NewMemoryStore()

	acc := &models.Account{ID: "acc-1", Provider: "openai"}
	quota := &models.QuotaInfo{AccountID: "acc-1", Provider: "openai"}

	store.SetAccount(acc)
	store.SetQuota("acc-1", quota)

	store.Clear()

	_, ok := store.GetAccount("acc-1")
	assert.False(t, ok)

	_, ok = store.GetQuota("acc-1")
	assert.False(t, ok)
}

func TestMemoryStore_Stats(t *testing.T) {
	store := NewMemoryStore()
	store.Clear()

	acc1 := &models.Account{ID: "acc-1", Provider: "openai"}
	acc2 := &models.Account{ID: "acc-2", Provider: "anthropic"}
	quota1 := &models.QuotaInfo{AccountID: "acc-1", Provider: "openai"}
	quota2 := &models.QuotaInfo{AccountID: "acc-2", Provider: "anthropic"}

	store.SetAccount(acc1)
	store.SetAccount(acc2)
	store.SetQuota("acc-1", quota1)
	store.SetQuota("acc-2", quota2)

	stats := store.Stats()
	assert.Equal(t, 2, stats.AccountCount)
	assert.Equal(t, 2, stats.QuotaCount)
}

func TestMemoryStore_ConcurrentAccess(t *testing.T) {
	store := NewMemoryStore()

	// Test concurrent writes
	done := make(chan bool, 10)

	for i := 0; i < 5; i++ {
		go func(id int) {
			acc := &models.Account{
				ID:       fmt.Sprintf("acc-%d", id),
				Provider: "openai",
			}
			store.SetAccount(acc)
			done <- true
		}(i)
	}

	for i := 0; i < 5; i++ {
		go func(id int) {
			quota := &models.QuotaInfo{
				AccountID: fmt.Sprintf("acc-%d", id),
				Provider:  "openai",
			}
			store.SetQuota(fmt.Sprintf("acc-%d", id), quota)
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify results
	stats := store.Stats()
	assert.Equal(t, 5, stats.AccountCount)
	assert.Equal(t, 5, stats.QuotaCount)
}
