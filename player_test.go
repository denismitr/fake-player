package player

import (
	"context"
	"testing"
	"time"
)

func TestPlayer(t *testing.T) {
	t.Run("add songs and play to the end", func(t *testing.T) {
		p, err := New()
		if err != nil {
			t.Fatal(err)
		}

		p.Add(context.Background(), Song{Name: "Song 1", Duration: 4 * time.Second})
		p.Add(context.Background(), Song{Name: "Song 2", Duration: 11 * time.Second})
		p.Add(context.Background(), Song{Name: "Song 3", Duration: 3 * time.Second})

		if n, err := p.Len(context.Background()); err != nil {
			t.Fatalf("unexpected error %v", err)
		} else if n != 3 {
			t.Errorf("expected n to be 3, got %d", n)
		}

		p.OnPlayTick(func(pos time.Duration, s Song) {
			t.Logf("song: %s, time: %.0f seconds", s.Name, pos.Seconds())
		})

		t.Log("starting to play")
		if err := p.Play(context.Background()); err != nil {
			t.Fatalf("unexpected error %v", err)
		}

		<-p.Stopped()
	})

	t.Run("init with songs", func(t *testing.T) {

		s1 := Song{Name: "Song 1", Duration: 4 * time.Second}
		s2 := Song{Name: "Song 2", Duration: 11 * time.Second}
		s3 := Song{Name: "Song 3", Duration: 3 * time.Second}

		p, err := New(s1, s2, s3)
		if err != nil {
			t.Fatal(err)
		}

		if n, err := p.Len(context.Background()); err != nil {
			t.Fatalf("unexpected error %v", err)
		} else if n != 3 {
			t.Errorf("expected n to be 3, got %d", n)
		}

		p.OnPlayTick(func(pos time.Duration, s Song) {
			t.Logf("song: %s, time: %.0f seconds", s.Name, pos.Seconds())
		})

		t.Log("starting to play")
		if err := p.Play(context.Background()); err != nil {
			t.Fatalf("unexpected error %v", err)
		}

		<-p.Stopped()
	})
}
