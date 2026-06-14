package ui_test

import (
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Joessst-Dev/queuetask/internal/ui"
)

var _ = Describe("Broadcaster", func() {
	var b *ui.Broadcaster

	BeforeEach(func() {
		b = ui.NewBroadcaster()
	})

	It("delivers a signal to a single subscriber after Notify", func() {
		ch, unsub := b.Subscribe()
		defer unsub()

		b.Notify()

		Eventually(ch, 100*time.Millisecond).Should(Receive())
	})

	It("fans out to all subscribers on a single Notify", func() {
		ch1, unsub1 := b.Subscribe()
		defer unsub1()
		ch2, unsub2 := b.Subscribe()
		defer unsub2()

		b.Notify()

		Eventually(ch1, 100*time.Millisecond).Should(Receive())
		Eventually(ch2, 100*time.Millisecond).Should(Receive())
	})

	It("stops delivering to an unsubscribed channel", func() {
		ch, unsub := b.Subscribe()
		unsub()

		b.Notify()

		Consistently(ch, 50*time.Millisecond).ShouldNot(Receive())
	})

	It("collapses rapid bursts to at most one pending signal", func() {
		ch, unsub := b.Subscribe()
		defer unsub()

		b.Notify()
		b.Notify()

		// Drain the one queued signal.
		Eventually(ch, 100*time.Millisecond).Should(Receive())
		// No second signal should be queued.
		Consistently(ch, 30*time.Millisecond).ShouldNot(Receive())
	})

	It("does not panic or block with no subscribers", func() {
		done := make(chan struct{})
		go func() {
			b.Notify()
			close(done)
		}()
		Eventually(done, 100*time.Millisecond).Should(BeClosed())
	})

	It("is safe for concurrent Subscribe, Notify, and Unsubscribe", func() {
		var wg sync.WaitGroup
		stop := make(chan struct{})

		// Writers: continuously notify.
		for range 5 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					select {
					case <-stop:
						return
					default:
						b.Notify()
					}
				}
			}()
		}

		// Readers: continuously subscribe and immediately unsubscribe.
		for range 5 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					select {
					case <-stop:
						return
					default:
						_, unsub := b.Subscribe()
						unsub()
					}
				}
			}()
		}

		time.Sleep(100 * time.Millisecond)
		close(stop)
		wg.Wait()
	})
})
