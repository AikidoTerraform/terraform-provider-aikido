package client_test

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/client"
)

func TestLoadCached(t *testing.T) {
	t.Run("runs load once per key", func(t *testing.T) {
		c := client.New(http.DefaultClient, "http://example.invalid", unlimited())
		var calls atomic.Int32

		load := func(context.Context) (int, error) {
			calls.Add(1)
			return 42, nil
		}

		got, err := client.LoadCached(c, context.Background(), "k", load)
		if err != nil || got != 42 {
			t.Fatalf("first = %d, %v", got, err)
		}

		got, err = client.LoadCached(c, context.Background(), "k", load)
		if err != nil || got != 42 {
			t.Fatalf("second = %d, %v", got, err)
		}

		if calls.Load() != 1 {
			t.Errorf("calls = %d, want 1", calls.Load())
		}
	})

	t.Run("separate keys load independently", func(t *testing.T) {
		c := client.New(http.DefaultClient, "http://example.invalid", unlimited())
		var a, b atomic.Int32

		if _, err := client.LoadCached(c, context.Background(), "a", func(context.Context) (string, error) {
			a.Add(1)
			return "A", nil
		}); err != nil {
			t.Fatal(err)
		}

		if _, err := client.LoadCached(c, context.Background(), "b", func(context.Context) (string, error) {
			b.Add(1)
			return "B", nil
		}); err != nil {
			t.Fatal(err)
		}

		if a.Load() != 1 || b.Load() != 1 {
			t.Errorf("a=%d b=%d, want 1 each", a.Load(), b.Load())
		}
	})

	t.Run("does not cache errors", func(t *testing.T) {
		c := client.New(http.DefaultClient, "http://example.invalid", unlimited())
		want := errors.New("boom")
		var calls atomic.Int32

		load := func(context.Context) (int, error) {
			n := calls.Add(1)
			if n == 1 {
				return 0, want
			}
			return 42, nil
		}

		if _, err := client.LoadCached(c, context.Background(), "err", load); !errors.Is(err, want) {
			t.Fatalf("first err = %v", err)
		}

		got, err := client.LoadCached(c, context.Background(), "err", load)
		if err != nil || got != 42 {
			t.Fatalf("retry = %d, %v", got, err)
		}
		if calls.Load() != 2 {
			t.Errorf("calls = %d, want 2", calls.Load())
		}

		got, err = client.LoadCached(c, context.Background(), "err", load)
		if err != nil || got != 42 {
			t.Fatalf("cached success = %d, %v", got, err)
		}
		if calls.Load() != 2 {
			t.Errorf("calls after success = %d, want 2", calls.Load())
		}
	})

	t.Run("singleflight on concurrent first load", func(t *testing.T) {
		c := client.New(http.DefaultClient, "http://example.invalid", unlimited())
		var calls atomic.Int32
		started := make(chan struct{})
		release := make(chan struct{})

		load := func(context.Context) (int, error) {
			n := calls.Add(1)
			if n == 1 {
				close(started)
			}
			<-release
			if n != 1 {
				return 0, errors.New("load invoked more than once")
			}
			return 7, nil
		}

		const n = 8
		var wg sync.WaitGroup
		errs := make(chan error, n)
		wg.Add(n)
		for range n {
			go func() {
				defer wg.Done()
				got, err := client.LoadCached(c, context.Background(), "concurrent", load)
				if err != nil || got != 7 {
					errs <- err
				}
			}()
		}

		<-started
		close(release)
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatal(err)
		}
		if calls.Load() != 1 {
			t.Errorf("calls = %d, want 1", calls.Load())
		}
	})
}
