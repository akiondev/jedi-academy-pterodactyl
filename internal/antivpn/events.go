package antivpn

import (
	"encoding/json"
	"net/netip"
	"regexp"
	"strings"
	"time"
)

// EventType identifies a parsed server-output event. Values are the
// stable JSON identifiers shipped over the addon NDJSON protocol; do
// not rename them without bumping the addon protocol version.
type EventType string

const (
	EventTypeRawLine               EventType = "raw_line"
	EventTypeInitGame              EventType = "init_game"
	EventTypeShutdownGame          EventType = "shutdown_game"
	EventTypeClientConnect         EventType = "client_connect"
	EventTypeClientDisconnect      EventType = "client_disconnect"
	EventTypeClientUserinfoChanged EventType = "client_userinfo_changed"
	EventTypeBadRcon               EventType = "bad_rcon"
	EventTypeChatMessage           EventType = "chat_message"
	EventTypeTeamChange            EventType = "team_change"
)

// EventSource identifies which underlying byte stream an event was
// observed on. The supervisor is the single owner/reader of the
// dedicated server's stdout and stderr; no event is ever sourced from
// a file in the new architecture.
type EventSource string

const (
	EventSourceStdout EventSource = "stdout"
	EventSourceStderr EventSource = "stderr"
)

// Event is the central event-bus payload. Fields are tagged with the
// stable JSON names used by the addon NDJSON protocol so addons can
// rely on them. Optional fields are emitted with `omitempty` so the
// per-event JSON stays small and so addons can use simple schemaless
// dict access (`event.get("ip")`) without conditionals.
type Event struct {
	Type      EventType   `json:"type"`
	Timestamp time.Time   `json:"time"`
	Source    EventSource `json:"source"`
	Raw       string      `json:"raw,omitempty"`

	Slot    string `json:"slot,omitempty"`
	IP      string `json:"ip,omitempty"`
	Port    int    `json:"port,omitempty"`
	Name    string `json:"name,omitempty"`
	Command string `json:"command,omitempty"`
	Message string `json:"message,omitempty"`
	OldTeam string `json:"old_team,omitempty"`
	NewTeam string `json:"new_team,omitempty"`
}

// MarshalNDJSON returns a single-line JSON encoding of the event with a
// trailing newline, suitable for writing directly to an addon process's
// stdin. The `encoding/json` standard library never emits embedded
// newlines for the field types we use, so each call produces exactly
// one NDJSON record.
func (e Event) MarshalNDJSON() ([]byte, error) {
	encoded, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

// newRawLineEvent builds a `raw_line` event for unconditional addon
// fan-out. Every captured line produces exactly one of these so addons
// that want a streaming feed of the dedicated server's console can
// subscribe without having to re-parse log files.
func newRawLineEvent(line string, source EventSource, ts time.Time) Event {
	return Event{
		Type:      EventTypeRawLine,
		Timestamp: ts,
		Source:    source,
		Raw:       line,
	}
}

func newClientConnectEvent(line string, source EventSource, ts time.Time, slot string, addr netip.Addr, name string) Event {
	ev := Event{
		Type:      EventTypeClientConnect,
		Timestamp: ts,
		Source:    source,
		Raw:       line,
		Slot:      slot,
		Name:      name,
	}
	if addr.IsValid() {
		ev.IP = addr.String()
	}
	return ev
}

func newClientDisconnectEvent(line string, source EventSource, ts time.Time, slot string) Event {
	return Event{
		Type:      EventTypeClientDisconnect,
		Timestamp: ts,
		Source:    source,
		Raw:       line,
		Slot:      slot,
	}
}

func newClientUserinfoChangedEvent(line string, source EventSource, ts time.Time, slot string, addr netip.Addr, name string) Event {
	ev := Event{
		Type:      EventTypeClientUserinfoChanged,
		Timestamp: ts,
		Source:    source,
		Raw:       line,
		Slot:      slot,
		Name:      name,
	}
	if addr.IsValid() {
		ev.IP = addr.String()
	}
	return ev
}

func newBadRconEvent(line string, source EventSource, ts time.Time, host string, ip netip.Addr, port int, command string) Event {
	ev := Event{
		Type:      EventTypeBadRcon,
		Timestamp: ts,
		Source:    source,
		Raw:       line,
		Port:      port,
		Command:   command,
	}
	if ip.IsValid() {
		ev.IP = ip.String()
	} else {
		// Surface the original textual host so addons can match against
		// non-numeric trusted-source lists (e.g. "localhost") without
		// having to reparse `Raw`.
		ev.IP = host
	}
	return ev
}

func newInitGameEvent(line string, source EventSource, ts time.Time) Event {
	return Event{
		Type:      EventTypeInitGame,
		Timestamp: ts,
		Source:    source,
		Raw:       line,
	}
}

func newShutdownGameEvent(line string, source EventSource, ts time.Time) Event {
	return Event{
		Type:      EventTypeShutdownGame,
		Timestamp: ts,
		Source:    source,
		Raw:       line,
	}
}

func newChatMessageEvent(line string, source EventSource, ts time.Time, slot, name, message string) Event {
	return Event{
		Type:      EventTypeChatMessage,
		Timestamp: ts,
		Source:    source,
		Raw:       line,
		Slot:      slot,
		Name:      name,
		Message:   message,
	}
}

// chatMessageMatch is the parsed result of a chat-shaped server output
// line. The supervisor's chat parser is intentionally narrow: it
// recognises the well-known stock JKA / TaystJK verbs (`say`,
// `sayteam`, `tell`) so that bundled addons can rely on a stable
// chat_message event for the common cases. Mod-specific verbs (e.g.
// JAPro `amsay` / `vsay`) are handled by addon-side classifiers
// reading the broader raw_line stream.
type chatMessageMatch struct {
	Slot    string
	Name    string
	Message string
}

var chatMessagePattern = regexp.MustCompile(
	`^(?:\s*` + logTimestampPrefixPattern + `\s+)?(?:say|sayteam|tell)\s*:\s*([^:]{1,64}?)\s*:\s*(.+)$`,
)

func parseChatMessage(line string) (chatMessageMatch, bool) {
	matches := chatMessagePattern.FindStringSubmatch(line)
	if len(matches) != 3 {
		return chatMessageMatch{}, false
	}
	name := strings.TrimSpace(matches[1])
	message := strings.TrimSpace(matches[2])
	if name == "" || message == "" {
		return chatMessageMatch{}, false
	}
	return chatMessageMatch{Name: name, Message: message}, true
}

// teamChangeMatch is the parsed shape of a TaystJK ChangeTeam line:
//
//	2026-04-25 15:12:32 ChangeTeam: 0 [90.144.88.223] (GUID) "akiondev" BLUE -> RED
//
// The leading timestamp and the GUID parenthetical are optional so the
// supervisor can also parse the older stock JKA / OpenJK shape.
type teamChangeMatch struct {
	Slot    string
	IP      string
	Name    string
	OldTeam string
	NewTeam string
}

var teamChangePattern = regexp.MustCompile(
	`^(?:\s*` + logTimestampPrefixPattern + `\s+)?ChangeTeam:\s*(\d+)\s*` +
		`(?:\[([^\]]*)\]\s*)?` +
		`(?:\([^)]*\)\s*)?` +
		`"([^"]*)"\s+` +
		`([A-Za-z]+)\s*->\s*([A-Za-z]+)\s*$`,
)

func parseTeamChange(line string) (teamChangeMatch, bool) {
	matches := teamChangePattern.FindStringSubmatch(line)
	if len(matches) != 6 {
		return teamChangeMatch{}, false
	}
	return teamChangeMatch{
		Slot:    matches[1],
		IP:      strings.TrimSpace(matches[2]),
		Name:    matches[3],
		OldTeam: strings.ToUpper(matches[4]),
		NewTeam: strings.ToUpper(matches[5]),
	}, true
}

func newTeamChangeEvent(line string, source EventSource, ts time.Time, m teamChangeMatch) Event {
	return Event{
		Type:      EventTypeTeamChange,
		Timestamp: ts,
		Source:    source,
		Raw:       line,
		Slot:      m.Slot,
		IP:        m.IP,
		Name:      m.Name,
		OldTeam:   m.OldTeam,
		NewTeam:   m.NewTeam,
	}
}

