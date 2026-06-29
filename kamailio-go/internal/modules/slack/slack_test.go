// SPDX-License-Identifier: GPL-2.0-or-later
/*
 * Kamailio-Go
 *
 * Slack module tests.
 */
package slack

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

func TestInitAndIsConnected(t *testing.T) {
	m := NewSlackModule()
	if m.IsConnected() {
		t.Fatal("expected not connected initially")
	}
	m.Init("")
	if m.IsConnected() {
		t.Error("expected not connected after empty Init")
	}
	m.Init("https://hooks.slack.com/services/T/B/X")
	if !m.IsConnected() {
		t.Error("expected connected after Init with URL")
	}
}

func TestSendAndSendAlert(t *testing.T) {
	m := NewSlackModule()
	// Sending while disconnected fails.
	if err := m.Send("hi"); err == nil {
		t.Error("expected error sending while disconnected")
	}
	if err := m.SendAlert("title", "body"); err == nil {
		t.Error("expected error sending alert while disconnected")
	}

	var gotURL, gotPayload string
	m.Init("https://hooks.example/T/B/X")
	m.SetPoster(func(url, payload string) error {
		gotURL = url
		gotPayload = payload
		return nil
	})

	if err := m.Send("hello"); err != nil {
		t.Fatalf("Send failed: %v", err)
	}
	if gotURL != "https://hooks.example/T/B/X" {
		t.Errorf("poster url = %q", gotURL)
	}
	if gotPayload != "hello" {
		t.Errorf("poster payload = %q, want hello", gotPayload)
	}

	if err := m.SendAlert("DiskFull", "no space"); err != nil {
		t.Fatalf("SendAlert failed: %v", err)
	}
	if !strings.Contains(gotPayload, "DiskFull") || !strings.Contains(gotPayload, "no space") {
		t.Errorf("alert payload = %q, want title+message", gotPayload)
	}
	if got := m.SentCount(); got != 2 {
		t.Errorf("SentCount = %d, want 2", got)
	}
}

func TestSendPosterError(t *testing.T) {
	m := NewSlackModule()
	m.Init("https://hooks.example/T/B/X")
	boom := errors.New("network down")
	m.SetPoster(func(url, payload string) error { return boom })
	if err := m.Send("x"); err != boom {
		t.Errorf("Send err = %v, want %v", err, boom)
	}
	// Even when the poster fails, the message is recorded.
	if got := m.SentCount(); got != 1 {
		t.Errorf("SentCount = %d, want 1", got)
	}
	// nil poster restores default.
	m.SetPoster(nil)
	if err := m.Send("y"); err != nil {
		t.Errorf("Send after default poster failed: %v", err)
	}
}

func TestConcurrentSlack(t *testing.T) {
	m := NewSlackModule()
	m.Init("https://hooks.example/T/B/X")
	var wg sync.WaitGroup
	const goroutines = 20
	const perG = 10
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < perG; j++ {
				_ = m.Send("msg")
			}
		}()
	}
	wg.Wait()
	want := goroutines * perG
	if got := m.SentCount(); got != want {
		t.Errorf("SentCount = %d, want %d", got, want)
	}
}
