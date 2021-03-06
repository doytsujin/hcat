package hcat

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/hashicorp/hcat/dep"
	idep "github.com/hashicorp/hcat/internal/dependency"
	"github.com/pkg/errors"
)

func TestWatcherAdd(t *testing.T) {
	t.Run("updates-map", func(t *testing.T) {
		w := newWatcher(t)
		defer w.Stop()

		d := &idep.FakeDep{}
		if added := w.Add(d); !added {
			t.Fatal("expected add to return true")
		}

		if !w.Watching(d.String()) {
			t.Errorf("expected add to append to map")
		}
	})
	t.Run("exists", func(t *testing.T) {
		w := newWatcher(t)
		defer w.Stop()

		d := &idep.FakeDep{}
		if added := w.Add(d); !added {
			t.Errorf("expected add to return true")
		}
		if added := w.Add(d); added {
			t.Errorf("expected add to return false")
		}
	})
	t.Run("startsViewPoll", func(t *testing.T) {
		w := newWatcher(t)
		defer w.Stop()

		if added := w.Add(&idep.FakeDep{}); !added {
			t.Errorf("expected add to return true")
		}

		select {
		case err := <-w.errCh:
			t.Fatal(err)
		case <-w.dataCh:
			// Got data, which means the poll was started
		}
	})
}

func TestWatcherWatching(t *testing.T) {
	t.Run("not-exists", func(t *testing.T) {
		w := newWatcher(t)
		defer w.Stop()

		d := &idep.FakeDep{}
		if w.Watching(d.String()) == true {
			t.Errorf("expected to not be Watching")
		}
	})

	t.Run("exists", func(t *testing.T) {
		w := newWatcher(t)
		defer w.Stop()

		d := &idep.FakeDep{}
		w.Add(d)

		if w.Watching(d.String()) == false {
			t.Errorf("expected to be Watching")
		}
	})
}

func TestWatcherRemove(t *testing.T) {
	t.Run("exists", func(t *testing.T) {
		w := newWatcher(t)
		defer w.Stop()

		d := &idep.FakeDep{}
		w.Add(d)

		removed := w.remove(d.String())
		if removed != true {
			t.Error("expected Remove to return true")
		}

		if w.Watching(d.String()) {
			t.Error("expected dependency to be removed")
		}
	})

	t.Run("does-not-exist", func(t *testing.T) {
		w := newWatcher(t)
		defer w.Stop()

		var fd idep.FakeDep
		removed := w.remove(fd.String())
		if removed != false {
			t.Fatal("expected Remove to return false")
		}
	})
}

func TestWatcherVaultToken(t *testing.T) {
	t.Run("empty-token", func(t *testing.T) {
		w := newWatcher(t)
		defer w.Stop()
		err := w.WatchVaultToken("")
		if err != nil {
			t.Fatal("Didn't expect and error:", err)
		}
		if w.Size() > 0 {
			t.Fatal("dependency should not have been added")
		}
	})
	t.Run("token-added", func(t *testing.T) {
		w := newWatcher(t)
		defer w.Stop()
		err := w.WatchVaultToken("fake-token")
		if err != nil {
			t.Fatal("Didn't expect and error:", err)
		}
		test_id := (&idep.VaultTokenQuery{}).String()

		if !w.Watching(test_id) {
			t.Fatal("token dep not added to watcher")
		}
	})
	t.Run("not-cleaned", func(t *testing.T) {
		w := newWatcher(t)
		defer w.Stop()
		err := w.WatchVaultToken("fake-token")
		if err != nil {
			t.Fatal("Didn't expect and error:", err)
		}
		test_id := (&idep.VaultTokenQuery{}).String()
		if !w.Watching(test_id) {
			t.Fatal("token dep not added to watcher")
		}
		stop := make(chan struct{})
		w.cleanDeps(stop)
		if !w.Watching(test_id) {
			t.Fatal("token dep should not have been cleaned")
		}
	})
}

func TestWatcherSize(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		w := newWatcher(t)
		defer w.Stop()

		if w.Size() != 0 {
			t.Errorf("expected %d to be %d", w.Size(), 0)
		}
	})

	t.Run("returns-num-views", func(t *testing.T) {
		w := newWatcher(t)
		defer w.Stop()

		for i := 0; i < 10; i++ {
			d := &idep.FakeDep{Name: fmt.Sprintf("%d", i)}
			w.Add(d)
		}

		if w.Size() != 10 {
			t.Errorf("expected %d to be %d", w.Size(), 10)
		}
	})
}

func TestWatcherWait(t *testing.T) {
	t.Run("timeout", func(t *testing.T) {
		w := newWatcher(t)
		defer w.Stop()
		t1 := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), time.Microsecond*100)
		defer cancel()
		err := w.Wait(ctx)
		if err != nil {
			t.Fatal("Error not expected")
		}
		dur := time.Now().Sub(t1)
		if dur < time.Microsecond*100 || dur > time.Millisecond*10 {
			t.Fatal("Wait call was off;", dur)
		}
	})
	t.Run("deadline", func(t *testing.T) {
		w := newWatcher(t)
		defer w.Stop()
		t1 := time.Now()
		ctx, cancel := context.WithDeadline(context.Background(),
			time.Now().Add(time.Microsecond*100))
		defer cancel()
		err := w.Wait(ctx)
		if err != nil {
			t.Fatal("Error not expected")
		}
		dur := time.Now().Sub(t1)
		if dur < time.Microsecond*100 || dur > time.Millisecond*10 {
			t.Fatal("Wait call was off;", dur)
		}
	})
	t.Run("cancel", func(t *testing.T) {
		w := newWatcher(t)
		defer w.Stop()

		errCh := make(chan error)
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		go func() {
			err := w.Wait(ctx)
			if err != nil {
				errCh <- err
			}
		}()
		cancel()
		err := <-errCh
		if ctx.Err() != context.Canceled {
			t.Fatal("unexpected context error:", ctx.Err())
		}
		if err != ctx.Err() {
			t.Fatal("unexpected wait error:", err)
		}
	})
	t.Run("0-timeout", func(t *testing.T) {
		w := newWatcher(t)
		defer w.Stop()
		t1 := time.Now()
		testerr := errors.New("test")
		go func() {
			time.Sleep(time.Microsecond * 100)
			w.errCh <- testerr
		}()
		w.Wait(context.Background())
		dur := time.Now().Sub(t1)
		if dur < time.Microsecond*100 || dur > time.Millisecond*10 {
			t.Fatal("Wait call was off;", dur)
		}
	})
	t.Run("error", func(t *testing.T) {
		w := newWatcher(t)
		defer w.Stop()
		testerr := errors.New("test")
		go func() {
			w.errCh <- testerr
		}()
		err := w.Wait(context.Background())
		if err != testerr {
			t.Fatal("None or Unexpected Error;", err)
		}
	})
	t.Run("remove-old-dependency", func(t *testing.T) {
		w := newWatcher(t)
		defer w.Stop()
		d := &idep.FakeDep{}
		w.Add(d)
		if !w.Watching(d.String()) {
			t.Error("expected dependency to be present")
		}
		w.cleanDeps(nil)
		if w.Watching(d.String()) {
			t.Error("expected dependency to be removed")
		}
	})
	// Test cache updates
	t.Run("simple-update", func(t *testing.T) {
		w := newWatcher(t)
		defer w.Stop()
		foodep := &idep.FakeDep{Name: "foo"}
		view := newView(&newViewInput{
			Dependency: foodep,
		})
		w.dataCh <- view
		w.Wait(context.Background())
		store := w.cache.(*Store)
		if _, ok := store.data[foodep.String()]; !ok {
			t.Fatal("failed update")
		}
	})
	t.Run("multi-update", func(t *testing.T) {
		w := newWatcher(t)
		defer w.Stop()
		deps := make([]dep.Dependency, 5)
		views := make([]*view, 5)
		for i := 0; i < 5; i++ {
			deps[i] = &idep.FakeDep{Name: strconv.Itoa(i)}
			views[i] = newView(&newViewInput{
				Dependency: deps[i],
			})
		}
		go func() {
			for _, v := range views {
				w.dataCh <- v
			}
		}()
		w.Wait(context.Background())
		store := w.cache.(*Store)
		if len(store.data) != 5 {
			t.Fatal("failed update")
		}
		if _, ok := store.data[deps[3].String()]; !ok {
			t.Fatal("failed update")
		}
	})
	// test tracking of updated dependencies
	t.Run("simple-updated-tracking", func(t *testing.T) {
		w := newWatcher(t)
		defer w.Stop()
		foodep := &idep.FakeDep{Name: "foo"}
		view := newView(&newViewInput{
			Dependency: foodep,
		})
		w.dataCh <- view
		w.Wait(context.Background())
		if w.changed.Len() != 1 {
			t.Fatal("failed to track updated dependency")
		}
	})
	t.Run("multi-updated-tracking", func(t *testing.T) {
		w := newWatcher(t)
		defer w.Stop()
		deps := make([]dep.Dependency, 5)
		views := make([]*view, 5)
		for i := 0; i < 5; i++ {
			deps[i] = &idep.FakeDep{Name: strconv.Itoa(i)}
			views[i] = newView(&newViewInput{
				Dependency: deps[i],
			})
		}
		go func() {
			for _, v := range views {
				w.dataCh <- v
			}
		}()
		w.Wait(context.Background())
		if w.changed.Len() != 5 {
			t.Fatal("failed to track updated dependency")
		}
	})
	t.Run("duplicate-updated-tracking", func(t *testing.T) {
		w := newWatcher(t)
		defer w.Stop()
		for i := 0; i < 2; i++ {
			foodep := &idep.FakeDep{Name: "foo"}
			view := newView(&newViewInput{
				Dependency: foodep,
			})
			w.dataCh <- view
		}
		w.Wait(context.Background())
		if w.changed.Len() != 1 {
			t.Fatal("failed to track updated dependency")
		}
	})
	t.Run("wait-channel", func(t *testing.T) {
		w := newWatcher(t)
		defer w.Stop()
		foodep := &idep.FakeDep{Name: "foo"}
		view := newView(&newViewInput{
			Dependency: foodep,
		})
		w.dataCh <- view
		err := <-w.WaitCh(context.Background())
		if err != nil {
			t.Fatal("wait error:", err)
		}
		store := w.cache.(*Store)
		if _, ok := store.data[foodep.String()]; !ok {
			t.Fatal("failed update")
		}
	})
	t.Run("wait-channel-cancel", func(t *testing.T) {
		w := newWatcher(t)
		defer w.Stop()

		errCh := make(chan error)
		ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
		go func() {
			select {
			case err := <-w.WaitCh(ctx):
				errCh <- err
			}
		}()
		cancel()
		err := <-errCh
		if ctx.Err() != context.Canceled {
			t.Fatal("unexpected context error:", ctx.Err())
		}
		if err != ctx.Err() {
			t.Fatal("unexpected wait error:", err)
		}
	})
	t.Run("wait-stop-leak", func(t *testing.T) {
		w := newWatcher(t)
		errCh := make(chan error)
		go func() {
			errCh <- w.Wait(context.Background())
		}()
		leaked := make(chan bool, 1)
		defer close(leaked)
		<-w.waitingCh
		w.Stop()
		select {
		case <-errCh:
			leaked <- false
		case <-time.After(time.Millisecond):
			leaked <- true
		}
		if ok := <-leaked; ok {
			t.Fatal("goroutine leak")
		}
	})
	t.Run("wait-stop-order", func(t *testing.T) {
		w := newWatcher(t)
		// can Stop can be run before Wait and have Wait work correctly
		w.Stop()
		errCh := make(chan error)
		go func() {
			errCh <- w.Wait(context.Background())
		}()
		bad_stop := make(chan bool, 1)
		defer close(bad_stop)
		<-w.waitingCh
		select {
		case <-errCh:
			bad_stop <- true
		case <-time.After(time.Millisecond):
			bad_stop <- false
		}
		if ok := <-bad_stop; ok {
			t.Fatal("Stop->Wait shouldn't stop Wait")
		}
		w.Stop()
	})
}

func newWatcher(t *testing.T) *Watcher {
	return NewWatcher(WatcherInput{
		Clients: NewClientSet(),
		Cache:   NewStore(),
	})
}
