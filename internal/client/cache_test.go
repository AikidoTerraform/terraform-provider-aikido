package client_test

import (
	"context"
	"errors"
	"net/http"
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

	t.Run("caches errors", func(t *testing.T) {
		c := client.New(http.DefaultClient, "http://example.invalid", unlimited())
		want := errors.New("boom")
		var calls atomic.Int32

		load := func(context.Context) (int, error) {
			calls.Add(1)
			return 0, want
		}

		if _, err := client.LoadCached(c, context.Background(), "err", load); !errors.Is(err, want) {
			t.Fatalf("first err = %v", err)
		}
		if _, err := client.LoadCached(c, context.Background(), "err", load); !errors.Is(err, want) {
			t.Fatalf("second err = %v", err)
		}
		if calls.Load() != 1 {
			t.Errorf("calls = %d, want 1", calls.Load())
		}
	})
}
