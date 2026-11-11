package main

import (
	"strings"
	"testing"

	"phoenix/internal/provider"
)

func resetQueue() {
	chatQueue.items, chatQueue.steer, chatQueue.carry, chatQueue.next = nil, false, "", 0
	chat.busy, chat.cancel, chat.history = false, nil, nil
}

func TestQueueKeepsOrderAndCapsLength(t *testing.T) {
	resetQueue()
	for i := 0; i < chatQueueMax+5; i++ {
		enqueueTurn("m", "m", false)
	}
	if len(chatQueue.items) != chatQueueMax {
		t.Fatalf("queue length %d, want %d", len(chatQueue.items), chatQueueMax)
	}
	if chatQueue.items[0].id != 1 || chatQueue.items[1].id != 2 {
		t.Fatal("queue must keep first-come order")
	}
	removeQueued(1)
	if len(chatQueue.items) != chatQueueMax-1 || chatQueue.items[0].id != 2 {
		t.Fatal("remove should drop only that message")
	}
	clearQueue()
	if len(chatQueue.items) != 0 {
		t.Fatal("clear should empty the queue")
	}
}

func TestSteerMovesMessageFirstAndInterruptsTheRun(t *testing.T) {
	resetQueue()
	defer resetQueue()
	interrupted := false
	chat.busy = true
	chat.cancel = func() { interrupted = true }
	enqueueTurn("one", "one", false)
	enqueueTurn("two", "two", false)
	steerQueued(2)
	if !interrupted || !chatQueue.steer {
		t.Fatal("steer must cancel the running turn and mark the queue")
	}
	if chatQueue.items[0].text != "two" || chatQueue.items[1].text != "one" {
		t.Fatalf("steered message must run first, got %v", chatQueue.items)
	}
	steerQueued(99) // unknown id: nothing happens
	if len(chatQueue.items) != 2 {
		t.Fatal("unknown id changed the queue")
	}
}

func TestContextWindowCompactionThreshold(t *testing.T) {
	resetQueue()
	defer resetQueue()
	chat.history = []provider.Message{{Role: "user", Content: "hi"}, {Role: "assistant", Content: "hello"}}
	if needsCompaction("next") {
		t.Fatal("a short chat must not compact")
	}
	big := strings.Repeat("x", 4*(chatCompactAt-chatSystemTokens))
	chat.history = []provider.Message{{Role: "user", Content: big}, {Role: "assistant", Content: "ok"}}
	if !needsCompaction("next") {
		t.Fatal("a chat filling the window must compact")
	}
	chat.history = chat.history[:1]
	if needsCompaction("next") {
		t.Fatal("nothing to summarize without a full exchange")
	}
}

func TestContinuationBlockCarriesSummaryAndLastExchange(t *testing.T) {
	h := []provider.Message{
		{Role: "user", Content: "old question"}, {Role: "assistant", Content: "old answer"},
		{Role: "user", Content: "latest question"}, {Role: "assistant", Content: "latest answer"},
	}
	got := continuationBlock(h, "SUMMARY TEXT")
	for _, want := range []string{"SUMMARY TEXT", "latest question", "latest answer", "new message follows"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
	if strings.Contains(got, "old question") {
		t.Error("only the last exchange is carried verbatim")
	}
}

func TestSummaryFeedRetainsRecentMessagesAndEnds(t *testing.T) {
	h := []provider.Message{{Role: "user", Content: "original goal"}}
	for i := 0; i < 120; i++ {
		h = append(h, provider.Message{Role: "assistant", Content: strings.Repeat("x", 7000)})
	}
	h = append(h, provider.Message{Role: "user", Content: strings.Repeat("a", 7000) + " FINAL CONSTRAINT"})
	got := summaryFeed(h)
	for _, want := range []string{"original goal", "FINAL CONSTRAINT", "middle messages omitted"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary feed missing %q", want)
		}
	}
	if len(got) > chatSummaryFeed+100 {
		t.Fatalf("feed exceeds budget: %d", len(got))
	}
}

func TestDigestKeepsEndOfLongMessage(t *testing.T) {
	got := condenseHistory([]provider.Message{{Role: "user", Content: strings.Repeat("é", 200) + " FINAL CONSTRAINT"}})
	if !strings.Contains(got, "FINAL CONSTRAINT") {
		t.Fatalf("missing late constraint: %q", got)
	}
}
