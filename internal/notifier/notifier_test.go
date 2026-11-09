package notifier

import (
	"sync/atomic"
	"testing"
	"time"

	"seanime/internal/database/models"

	"github.com/stretchr/testify/require"
)

func TestCanProceed(t *testing.T) {
	t.Run("returns false without settings", func(t *testing.T) {
		n := NewNotifier()

		require.False(t, n.canProceed(AutoDownloader))
	})

	t.Run("respects global disable", func(t *testing.T) {
		n := NewNotifier()
		n.SetSettings("/tmp", &models.NotificationSettings{DisableNotifications: true}, nil)

		require.False(t, n.canProceed(Debrid))
	})

	t.Run("respects auto downloader disable", func(t *testing.T) {
		n := NewNotifier()
		n.SetSettings("/tmp", &models.NotificationSettings{DisableAutoDownloaderNotifications: true}, nil)

		require.False(t, n.canProceed(AutoDownloader))
		require.True(t, n.canProceed(Debrid))
	})

	t.Run("respects auto scanner disable", func(t *testing.T) {
		n := NewNotifier()
		n.SetSettings("/tmp", &models.NotificationSettings{DisableAutoScannerNotifications: true}, nil)

		require.False(t, n.canProceed(AutoScanner))
		require.True(t, n.canProceed(Debrid))
	})
}

func TestNotify(t *testing.T) {
	// YOZORA (fork): the test config file is optional (gitignored); the notifier
	// only needs a data directory to build its logo path, so use a temp dir
	// instead of depending on test/config.toml.
	datadir := t.TempDir()
	t.Run("pushes when enabled", func(t *testing.T) {
		n := NewNotifier()
		n.SetSettings(datadir, &models.NotificationSettings{}, nil)

		called := make(chan struct{}, 1)
		n.push = func(title, message, icon string) error {
			require.Equal(t, "Seanime: Debrid", title)
			require.Equal(t, "downloaded", message)
			called <- struct{}{}
			return nil
		}

		n.Notify(Debrid, "downloaded")

		select {
		case <-called:
		case <-time.After(time.Second):
			t.Fatal("expected notifier to push notification")
		}
	})

	t.Run("skips push when disabled", func(t *testing.T) {
		n := NewNotifier()
		n.SetSettings(datadir, &models.NotificationSettings{DisableNotifications: true}, nil)

		var calls atomic.Int32
		n.push = func(title, message, icon string) error {
			calls.Add(1)
			return nil
		}

		n.Notify(Debrid, "downloaded")

		require.Eventually(t, func() bool {
			return calls.Load() == 0
		}, 200*time.Millisecond, 20*time.Millisecond)
	})
}
