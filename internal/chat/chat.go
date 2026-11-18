// Package chat is the data side of sltty: conversations, messages and
// threads, behind a Backend so the TUI never knows where they come from.
package chat

import (
	"context"
	"strings"
	"time"
)

// Kind tells channels from direct messages.
type Kind int

const (
	Channel Kind = iota
	DM
)

// Conversation is one entry of the sidebar.
type Conversation struct {
	ID      string
	Name    string
	Kind    Kind
	Topic   string
	Unread  int
	Online  bool // DMs: the other person is active
	Private bool // channels: invite only
	// Section and SectionEmoji come from Slack's native sidebar sections.
	// Their order is already reflected in the Conversations result.
	Section      string
	SectionEmoji string
	// Muted: its unread messages count quietly (a grey badge).
	Muted bool
	// Pinned keeps its place: new messages do not move it up.
	Pinned bool
	// Folded: its section starts folded (Telegram's Archived).
	Folded bool
	// VIP: a direct message with one of the user's VIPs (Slack).
	VIP bool
	// Self: the chat with yourself (Telegram's Saved Messages, Slack's
	// DM to yourself), always listed.
	Self bool
	// Group: a group DM (Slack's mpdm), with this many people besides you.
	Group int
}

// ThreadSummary is a thread the user takes part in, for the Threads view.
type ThreadSummary struct {
	ConvID  string
	Root    Message
	Latest  []Message // the last replies, oldest first
	Replies int
	Unread  int // replies by others since the user's last message
}

// ActivityKind is what an Activity item is about.
type ActivityKind int

const (
	ActMention  ActivityKind = iota // someone wrote @you
	ActReply                        // a reply in a thread the user takes part in
	ActReaction                     // someone reacted to the user's message
	ActDM                           // a direct message (a person's, or an app's)
)

// Activity is one entry of the Activity view.
type Activity struct {
	ID     string
	Kind   ActivityKind
	ConvID string
	RootTS string  // the thread, for replies (and for messages inside one)
	Msg    Message // the mention, the reply, or the message reacted to
	By     string
	ByID   string // actor's Slack ID, for an avatar
	Emoji  string // reactions
	At     time.Time
	Unread bool
}

// Reaction is one emoji under a message and how many people used it.
type Reaction struct {
	Emoji string
	Count int
	Mine  bool     // the user is one of them
	Users []string // display names of the people who reacted
}

// Message is one message of a conversation or of a thread. TS identifies it
// within its conversation, as Slack's ts does.
type Message struct {
	TS     string
	User   string
	UserID string // for the avatar: File("avatar:" + UserID)
	Text   string // as people read it
	// Raw is the markup behind Text (mentions, links) when it differs:
	// what an edit starts from.
	Raw         string
	At          time.Time
	Reactions   []Reaction
	Replies     int       // thread replies; only on a thread's root
	LastReply   time.Time // when Replies > 0
	Edited      bool
	Files       []File       // uploaded files
	Attachments []Attachment // Slack message attachments/cards
	Context     []string     // the small print under it (Block Kit context)
	// Quote is the message it answers (a Telegram reply), drawn above it.
	Quote *Quote
}

// Quote is a message another one answers: who wrote it and its beginning.
// Who is empty when it is not known (an old message).
type Quote struct {
	Who, Text string
}

// File is an attachment. Images have their size; the bytes come from
// Backend.File.
type File struct {
	ID   string
	Name string
	Mime string
	W, H int // pixels, for images
}

func (f File) IsImage() bool { return strings.HasPrefix(f.Mime, "image/") }

// Attachment is Slack's legacy rich card shape, used heavily by apps such as
// Sentry and GitLab. Text remains mrkdwn; the renderer owns presentation.
type Attachment struct {
	Color, Pretext, Title, TitleURL, Text, Footer string
	Fields                                        []AttachmentField
}
type AttachmentField struct {
	Title, Value string
	Short        bool
}

// Hit is a search result: a message, where it lives and, for a thread
// reply, its root.
type Hit struct {
	ConvID string
	RootTS string // empty for a message of the conversation itself
	Msg    Message
}

// Backend is what the TUI needs from a chat service. A real Slack backend
// goes here once a workspace app is approved; until then Fake stands in.
// Query is a search split the way Slack reads it: words, and the in: and
// from: modifiers (in:#channel, in:@person, from:@person).
type Query struct {
	Words    []string
	In, From string
}

func ParseQuery(q string) Query {
	var out Query
	for _, f := range strings.Fields(q) {
		low := strings.ToLower(f)
		switch {
		case strings.HasPrefix(low, "in:") && len(f) > 3:
			out.In = strings.TrimLeft(low[3:], "#@")
		case strings.HasPrefix(low, "from:") && len(f) > 5:
			out.From = strings.TrimLeft(low[5:], "@")
		default:
			out.Words = append(out.Words, f)
		}
	}
	return out
}

type Backend interface {
	Me() string
	Workspace() string
	Conversations(ctx context.Context) ([]Conversation, error)
	History(ctx context.Context, convID string) ([]Message, error)
	// Replies returns a thread: its root first, then the replies.
	Replies(ctx context.Context, convID, rootTS string) ([]Message, error)
	// Send posts text to the conversation, or to the thread of rootTS when
	// rootTS is not empty.
	Send(ctx context.Context, convID, rootTS, text string) (Message, error)
	// Edit replaces the text of one of the user's own messages.
	Edit(ctx context.Context, convID, ts, text string) (Message, error)
	// React adds the user's emoji to a message, or takes it back.
	React(ctx context.Context, convID, ts, emoji string) (Message, error)
	// Search finds messages containing query, newest first.
	Search(ctx context.Context, query string) ([]Hit, error)
	// Threads lists the threads the user takes part in, latest reply first.
	Threads(ctx context.Context) ([]ThreadSummary, error)
	// Activity lists mentions, replies and reactions for the user, newest
	// first.
	Activity(ctx context.Context) ([]Activity, error)
	// Mark moves the read cursor of a conversation to ts: what comes after
	// it is unread (conversations.mark). The caller counts what that leaves
	// unread: Slack does not say.
	Mark(ctx context.Context, convID, ts string) error
	// File returns an attachment's picture (a thumbnail; "full:" + the ID
	// for the original), a custom emoji's image for "emoji:name" or a
	// person's avatar for "avatar:" + Message.UserID (files:read,
	// emoji:read, users:read). Callers keep them in memory only.
	File(ctx context.Context, id string) ([]byte, error)
	// CustomEmoji lists the workspace's own :name: emoji.
	CustomEmoji(ctx context.Context) ([]string, error)
	// Presence reports whether the current user is away.
	Presence(ctx context.Context) (away bool, err error)
	// SetPresence shows the user away or active (users.setPresence).
	SetPresence(ctx context.Context, away bool) error
	// SetSnooze enables Do Not Disturb for minutes; zero ends it.
	SetSnooze(ctx context.Context, minutes int) error
}

// EventKind is what a live Event reports.
type EventKind int

const (
	EvMessage    EventKind = iota // a new message (Msg; RootTS when it answers one)
	EvEdited                      // a message changed (Msg)
	EvReactions                   // the reactions of Msg.TS are now Msg.Reactions
	EvDeleted                     // the messages TS are gone
	EvRead                        // the conversation was read elsewhere: Unread are left
	EvPresence                    // a direct message's person is Online or not
	EvTyping                      // User is typing (Action says what; empty: stopped)
	EvConv                        // Conv changed (moved to or from Archived, muted...)
	EvUnreadMark                  // the conversation was marked unread (Unread 1) or not (0) elsewhere
)

// Event is something that happened while the client runs.
type Event struct {
	Kind   EventKind
	ConvID string
	// Conv describes the conversation of an EvMessage, for one the
	// sidebar does not have yet.
	Conv   *Conversation
	RootTS string
	Msg    Message
	TS     []string
	Unread int
	Online bool
	User   string // EvTyping
	Action string // EvTyping: "typing", "recording a voice message"...
}

// UnreadMarker is a Backend that marks a whole conversation unread on the
// server (Telegram's "Mark as unread"); its read cursor only moves forward.
// Mark clears the mark.
type UnreadMarker interface {
	MarkUnread(ctx context.Context, convID string, unread bool) error
}

// Counter is a Backend without live updates that can tell every
// conversation's unread count in one cheap call, to poll (Slack).
type Counter interface {
	UnreadCounts(ctx context.Context) (map[string]int, error)
}

// Replier is a Backend whose replies quote a message in the conversation
// itself (Telegram's reply), besides threads: Reply posts text answering ts.
type Replier interface {
	Reply(ctx context.Context, convID, ts, text string) (Message, error)
}

// Upload is a file to send: its name and its bytes. ReplyTo, on a
// Replier's backend, is the message it answers (Telegram's reply).
type Upload struct {
	Name    string
	Data    []byte
	ReplyTo string
}

// Uploader is a Backend that sends files: Upload posts f with text as its
// caption, in the conversation or in thread rootTS. A Message without TS
// means the service did not tell it: the conversation shows it on reload.
type Uploader interface {
	Upload(ctx context.Context, convID, rootTS, text string, f Upload) (Message, error)
}

// Person is someone a message can mention: Name is how the mention reads
// (after the @), Full their full name, for finding and showing them.
type Person struct {
	ID, Name, Full string
}

// Mentioner is a Backend whose messages can mention people. People finds
// whom: the conversation's members first, then anyone matching query
// (empty: just the members). In the text Send, Reply, Upload and Edit take,
// a mention is written <@ID>, as Slack does; each service makes it its own.
type Mentioner interface {
	People(ctx context.Context, convID, query string) ([]Person, error)
	// PersonName is the name a mention of id reads as, when known.
	PersonName(id string) (string, bool)
}

// DMOpener opens (or returns) a direct-message conversation with a person.
// Slack creates the DM lazily, so it is not otherwise present in Conversations.
type DMOpener interface {
	OpenDM(ctx context.Context, personID string) (Conversation, error)
}

// Typer is a Backend that tells others the user is typing.
type Typer interface {
	Typing(ctx context.Context, convID, rootTS string) error
}

// Threadless is a Backend without a list of the user's threads: the
// Threads view is hidden.
type Threadless interface {
	NoThreadsView()
}

// Live is a Backend that pushes events as they happen. The channel stays
// open while the backend runs.
type Live interface {
	Events() <-chan Event
}
