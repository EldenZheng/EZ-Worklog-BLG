package main

import (
	"errors"
	"reflect"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestProjectBoardsOverlapWithBoundedConcurrency(t *testing.T) {
	started := make(chan string, 5)
	release := make(chan struct{})
	var released sync.Once
	releaseAll := func() { released.Do(func() { close(release) }) }
	defer releaseAll()
	done := make(chan []WorklogItem, 1)
	errResult := make(chan error, 1)
	var active, peak atomic.Int32
	go func() {
		items, err := fetchProjectBoards([]string{"0", "1", "2", "3", "4"}, "owner", "date filter",
			func(url, owner, filter string) ([]WorklogItem, error) {
				n := active.Add(1)
				defer active.Add(-1)
				for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
				}
				started <- url
				<-release
				if owner != "owner" || filter != "date filter" {
					return nil, errors.New("filters changed")
				}
				return []WorklogItem{{URL: url}}, nil
			})
		errResult <- err
		done <- items
	}()
	// No board can finish yet: seeing five start proves the reads overlap.
	// Concurrency is capped at eight, so five boards run all at once.
	for i := 0; i < 5; i++ {
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("board fetches did not overlap")
		}
	}
	releaseAll()
	var got []WorklogItem
	select {
	case got = <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("board reads did not complete")
	}
	if err := <-errResult; err != nil {
		t.Fatal(err)
	}
	if peak.Load() != 5 {
		t.Fatalf("peak concurrency = %d, want 5", peak.Load())
	}
	if len(got) != 5 {
		t.Fatalf("got %d boards, want 5", len(got))
	}
	for i, item := range got {
		if item.URL != strconv.Itoa(i) {
			t.Fatal("parallel reads changed configured order")
		}
	}
}

func TestProjectBoardsKeepDeduplicationAndRejectPartialTotals(t *testing.T) {
	first := WorklogItem{URL: "shared", Minutes: 120}
	second := WorklogItem{URL: "second", Minutes: 60}
	fetch := func(url, _, _ string) ([]WorklogItem, error) {
		if url == "first" {
			return []WorklogItem{first}, nil
		}
		return []WorklogItem{{URL: "shared", Minutes: 999}, second}, nil
	}
	got, err := fetchProjectBoards([]string{"first", "second"}, "", "", fetch)
	if err != nil || !reflect.DeepEqual(got, []WorklogItem{first, second}) {
		t.Fatalf("changed union: %v, %v", got, err)
	}
	want := errors.New("board unavailable")
	got, err = fetchProjectBoards([]string{"first", "failed"}, "", "", func(url, _, _ string) ([]WorklogItem, error) {
		if url == "failed" {
			return nil, want
		}
		return []WorklogItem{first}, nil
	})
	if got != nil || !errors.Is(err, want) {
		t.Fatalf("partial totals leaked: %v, %v", got, err)
	}
}
