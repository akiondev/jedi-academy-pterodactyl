package antivpn

import (
	"context"
	"encoding/json"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestEventMarshalNDJSONStableFieldNames(t *testing.T) {
	addr := netip.MustParseAddr("90.144.88.223")
	ev := newClientConnectEvent("ClientConnect: 0 [90.144.88.223] \"akiondev\"", EventSourceStdout, time.Unix(1700000000, 0).UTC(), "0", addr, "akiondev")

	data, err := ev.MarshalNDJSON()
	if err != nil {
		t.Fatalf("MarshalNDJSON: %v", err)
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Fatalf("expected trailing newline, got %q", string(data))
	}
	if strings.Count(string(data), "\n") != 1 {
		t.Fatalf("expected exactly one newline, got %q", string(data))
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json round-trip: %v", err)
	}
	if decoded["type"] != "client_connect" {
		t.Fatalf("type field = %v", decoded["type"])
	}
	if decoded["source"] != "stdout" {
		t.Fatalf("source field = %v", decoded["source"])
	}
	if decoded["slot"] != "0" {
		t.Fatalf("slot field = %v", decoded["slot"])
	}
	if decoded["ip"] != "90.144.88.223" {
		t.Fatalf("ip field = %v", decoded["ip"])
	}
	if decoded["name"] != "akiondev" {
		t.Fatalf("name field = %v", decoded["name"])
	}
}

func TestParseChatMessageRecognisesStockVerbs(t *testing.T) {
	cases := []struct {
		name string
		line string
		want chatMessageMatch
	}{
		{
			name: "say",
			line: "say: akiondev: hello world",
			want: chatMessageMatch{Name: "akiondev", Message: "hello world"},
		},
		{
			name: "sayteam",
			line: "sayteam: akiondev: regroup",
			want: chatMessageMatch{Name: "akiondev", Message: "regroup"},
		},
		{
			name: "tell",
			line: "tell: akiondev: psst",
			want: chatMessageMatch{Name: "akiondev", Message: "psst"},
		},
		{
			name: "with engine timestamp",
			line: "  3:42 say: akiondev: hello",
			want: chatMessageMatch{Name: "akiondev", Message: "hello"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseChatMessage(tc.line)
			if !ok {
				t.Fatalf("parseChatMessage(%q) = false", tc.line)
			}
			if got != tc.want {
				t.Fatalf("parseChatMessage(%q) = %+v, want %+v", tc.line, got, tc.want)
			}
		})
	}
}

func TestParseChatMessageRejectsNonChat(t *testing.T) {
	for _, line := range []string{
		"",
		"ClientConnect: 0 [1.2.3.4]",
		"Bad rcon from 1.2.3.4:29070: status",
		"InitGame: \\sv_hostname\\Test",
	} {
		if _, ok := parseChatMessage(line); ok {
			t.Fatalf("parseChatMessage(%q) unexpectedly matched", line)
		}
	}
}

type recordingHandler struct {
	name     string
	events   []Event
	mu       sync.Mutex
	delay    time.Duration
	released chan struct{}
	gate     chan struct{}
}

func (h *recordingHandler) Name() string { return h.name }
func (h *recordingHandler) HandleEvent(_ context.Context, ev Event) {
	if h.gate != nil {
		<-h.gate
	}
	if h.delay > 0 {
		time.Sleep(h.delay)
	}
	h.mu.Lock()
	h.events = append(h.events, ev)
	h.mu.Unlock()
	if h.released != nil {
		select {
		case h.released <- struct{}{}:
		default:
		}
	}
}

func (h *recordingHandler) snapshot() []Event {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]Event, len(h.events))
	copy(out, h.events)
	return out
}

func TestDispatcherDeliversEachEventOncePerHandler(t *testing.T) {
	d := NewEventDispatcher(nil, 16, EventDispatchDropOldest)
	defer d.Close()

	a := &recordingHandler{name: "a"}
	b := &recordingHandler{name: "b"}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	d.Subscribe(ctx, a)
	d.Subscribe(ctx, b)

	for i := 0; i < 3; i++ {
		d.Publish(Event{Type: EventTypeRawLine, Raw: "line"})
	}

	deadline := time.After(2 * time.Second)
	for {
		if len(a.snapshot()) == 3 && len(b.snapshot()) == 3 {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for delivery: a=%d b=%d", len(a.snapshot()), len(b.snapshot()))
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func TestDispatcherSlowHandlerDoesNotBlockPublish(t *testing.T) {
	d := NewEventDispatcher(nil, 4, EventDispatchDropOldest)
	defer d.Close()

	gate := make(chan struct{})
	slow := &recordingHandler{name: "slow", gate: gate}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d.Subscribe(ctx, slow)

	// Fill the queue beyond capacity while the handler is blocked.
	// Each Publish call must complete in well under the timeout below;
	// drop-oldest should kick in once the buffer is full.
	const calls = 100
	var wg sync.WaitGroup
	wg.Add(1)
	doneCh := make(chan struct{})
	go func() {
		defer wg.Done()
		for i := 0; i < calls; i++ {
			d.Publish(Event{Type: EventTypeRawLine})
		}
		close(doneCh)
	}()

	select {
	case <-doneCh:
	case <-time.After(2 * time.Second):
		t.Fatalf("Publish blocked despite drop-oldest policy")
	}
	wg.Wait()

	// Release the slow handler and let it drain whatever survived.
	close(gate)
}

func TestDispatcherClosedDoesNotPanicOnPublish(t *testing.T) {
	d := NewEventDispatcher(nil, 4, EventDispatchDropOldest)
	d.Close()
	if got := d.Publish(Event{Type: EventTypeRawLine}); got != 0 {
		t.Fatalf("Publish after Close delivered %d events, want 0", got)
	}
}

func TestParseEventDispatchPolicyDefaults(t *testing.T) {
	cases := map[string]EventDispatchPolicy{
		"":             EventDispatchDropOldest,
		"drop-oldest":  EventDispatchDropOldest,
		"drop-newest":  EventDispatchDropNewest,
		"unknown-junk": EventDispatchDropOldest,
	}
	for in, want := range cases {
		if got := ParseEventDispatchPolicy(in); got != want {
			t.Fatalf("ParseEventDispatchPolicy(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestSupervisorHandleLogLinePublishesParsedEvents(t *testing.T) {
	dispatcher := NewEventDispatcher(nil, 32, EventDispatchDropOldest)
	defer dispatcher.Close()

	collected := make(chan Event, 16)
	dispatcher.Subscribe(context.Background(), &funcHandler{
		name: "collector",
		fn: func(_ context.Context, ev Event) {
			collected <- ev
		},
	})

	s := newTestSupervisor(t, Config{Mode: ModeOff})
	s.dispatcher = dispatcher

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	lines := []string{
		"ClientConnect: 0 [1.2.3.4] \"akiondev\"",
		"ClientDisconnect: 0",
		"Bad rcon from 90.144.88.223:29070: status",
		"say: akiondev: hello",
	}
	for _, line := range lines {
		s.handleLogLine(ctx, &nullStdin{}, line, "stdout")
	}

	wantTypes := map[EventType]int{
		EventTypeClientConnect:    1,
		EventTypeClientDisconnect: 1,
		EventTypeBadRcon:          1,
		EventTypeChatMessage:      1,
	}
	got := map[EventType]int{}
	deadline := time.After(2 * time.Second)
	expected := 0
	for _, n := range wantTypes {
		expected += n
	}
loop:
	for received := 0; received < expected; {
		select {
		case ev := <-collected:
			got[ev.Type]++
			received++
		case <-deadline:
			break loop
		}
	}
	for ty, want := range wantTypes {
		if got[ty] != want {
			t.Fatalf("event %s: got %d, want %d (all=%v)", ty, got[ty], want, got)
		}
	}
}

type nullStdin struct{}

func (nullStdin) Write(p []byte) (int, error) { return len(p), nil }


type funcHandler struct {
	name string
	fn   func(context.Context, Event)
}

func (h *funcHandler) Name() string                            { return h.name }
func (h *funcHandler) HandleEvent(ctx context.Context, ev Event) { h.fn(ctx, ev) }

func TestParseTeamChangeMatchesTaystJKLine(t *testing.T) {
line := `2026-04-25 15:12:32 ChangeTeam: 0 [90.144.88.223] (324A7B4259866E7A4960FEC1F6BE407A) "akiondev" BLUE -> RED`
got, ok := parseTeamChange(line)
if !ok {
t.Fatalf("expected parseTeamChange to match line: %q", line)
}
want := teamChangeMatch{Slot: "0", IP: "90.144.88.223", Name: "akiondev", OldTeam: "BLUE", NewTeam: "RED"}
if got != want {
t.Fatalf("parseTeamChange mismatch: got %+v want %+v", got, want)
}
}

func TestParseTeamChangeMatchesStockJKAShape(t *testing.T) {
// Older / stock JKA shape without IP bracket and without GUID parens.
line := `ChangeTeam: 3 "Tester" SPECTATOR -> RED`
got, ok := parseTeamChange(line)
if !ok {
t.Fatalf("expected parseTeamChange to match line: %q", line)
}
if got.Slot != "3" || got.Name != "Tester" || got.OldTeam != "SPECTATOR" || got.NewTeam != "RED" {
t.Fatalf("parseTeamChange unexpected fields: %+v", got)
}
}

func TestParseTeamChangeIgnoresUnrelatedLine(t *testing.T) {
if _, ok := parseTeamChange(`ClientConnect: 0 [127.0.0.1]`); ok {
t.Fatalf("expected parseTeamChange to ignore non-ChangeTeam line")
}
}

func TestNewTeamChangeEventCarriesAllFields(t *testing.T) {
now := time.Now()
m := teamChangeMatch{Slot: "0", IP: "10.0.0.1", Name: "p1", OldTeam: "BLUE", NewTeam: "RED"}
ev := newTeamChangeEvent("raw", EventSourceStdout, now, m)
if ev.Type != EventTypeTeamChange {
t.Fatalf("unexpected event type %q", ev.Type)
}
if ev.Slot != "0" || ev.IP != "10.0.0.1" || ev.Name != "p1" || ev.OldTeam != "BLUE" || ev.NewTeam != "RED" {
t.Fatalf("event fields mismatch: %+v", ev)
}
}
