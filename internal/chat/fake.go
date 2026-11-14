package chat

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Fake is an in-memory Backend with made-up data, for trying the UI without
// touching any real workspace. Nothing is persisted.
type Fake struct {
	mu        sync.Mutex
	reactedBy []Activity // who reacted to the user's messages (Activity)
	convs     []Conversation
	msgs      map[string][]Message // by conversation
	replies   map[string][]Message // by conversation + "/" + root ts
	seq       int
	now       func() time.Time
}

// line is a scripted message: "user: text", optionally with reactions and
// thread replies.
type line struct {
	text      string
	reactions []Reaction
	replies   []string
	files     []string // fakeFiles IDs
	context   []string // app metadata below the message
}

func l(text string) line { return line{text: text} }

// fakeReactionUsers gives every demo reaction visible participants, like
// Slack's reactions payloads do for a real workspace.
func fakeReactionUsers(n int) []string {
	people := []string{"ana", "bob", "carol", "dave", "erin", "frank", "you"}
	n = min(n, len(people))
	return append([]string(nil), people[:max(0, n)]...)
}

// NewFake returns a Fake with a few demo conversations. Messages start
// yesterday afternoon, so the day separators show.
func NewFake() *Fake {
	f := &Fake{msgs: map[string][]Message{}, replies: map[string][]Message{}, now: time.Now}
	defer f.scriptReactions()
	y := time.Now().AddDate(0, 0, -1)
	yesterday := time.Date(y.Year(), y.Month(), y.Day(), 16, 0, 0, 0, time.Local)
	today := time.Now().Add(-2 * time.Hour)

	add := func(c Conversation, at time.Time, lines ...line) {
		if !f.has(c.ID) {
			f.convs = append(f.convs, c)
		}
		for i, ln := range lines {
			t := at.Add(time.Duration(i*4) * time.Minute)
			m := f.newMsg(ln.text, t)
			m.Reactions = ln.reactions
			for j := range m.Reactions {
				if len(m.Reactions[j].Users) == 0 {
					m.Reactions[j].Users = fakeReactionUsers(m.Reactions[j].Count)
				}
			}
			m.Context = ln.context
			for _, id := range ln.files {
				m.Files = append(m.Files, fakeFiles[id])
			}
			for j, r := range ln.replies {
				rm := f.newMsg(r, t.Add(time.Duration(j+1)*9*time.Minute))
				f.replies[c.ID+"/"+m.TS] = append(f.replies[c.ID+"/"+m.TS], rm)
				m.Replies, m.LastReply = j+1, rm.At
			}
			f.msgs[c.ID] = append(f.msgs[c.ID], m)
		}
	}

	general := Conversation{ID: "C1", Name: "general", Topic: "Company-wide announcements"}
	add(general, yesterday,
		line{text: "ana: the internal tools demo is on Friday; please add your topic to the shared doc", reactions: []Reaction{{Emoji: "👍", Count: 6}, {Emoji: "🎉", Count: 2}}},
		l("bob: will the session be recorded for people in other time zones?"),
		l("ana: yes, I will post the recording and notes afterwards"),
		l("carol: I can cover the terminal client walkthrough if that helps"))
	add(general, today,
		l("carol: good morning 👋"),
		line{text: "bob: does anyone know whether the staging runner is down?", replies: []string{
			"carol: I am checking it now",
			"carol: the runner was out of memory; it has been restarted",
			"bob: thank you!",
		}, reactions: []Reaction{{Emoji: "👀", Count: 2}}},
		l("dave: pipelines are green again"),
		l("erin: the deployment dashboard has caught up too"))

	tools := Conversation{ID: "C2", Name: "tools-team", Topic: "builds, auth, runners", Unread: 3}
	add(tools, yesterday,
		line{text: "dave: the new release dashboard is ready for review", reactions: []Reaction{{Emoji: "🚀", Count: 5}}, replies: []string{
			"ana: the pipeline layout reads really well",
			"you: the analytics view also works nicely at 80 columns",
			"dave: next up is grouping analytics by service",
		}},
		line{text: "ana: runner capacity for this week :sltty:", files: []string{"F1"}, reactions: []Reaction{{Emoji: ":shipit:", Count: 2}}})
	add(tools, today,
		line{text: "Sentry: :red_circle: [BuildError](https://sentry.example.test/issues/DEMO-7/)\nworker.jobs.publish_release in run\n```Git service rejected the request (400): invalid branch reference```\nenvironment: `production`\nState: New   First Seen: Just now\n[ Resolve ] [ Archive ] [ Select Assignee... ▾ ]\nnotes: Send a notification to [#deploys](https://example.test/deploys)\nProject: [demo](https://example.test/demo)   Alert: [Release alerts](https://example.test/alerting)   Short ID: DEMO-7", context: []string{"Sentry app · incident notification"}},
		l("erin: should we demo this on Friday?"),
		l("erin: it would be useful to include the runner workflow too"),
		line{text: "dave: sounds good; I will reserve a room", reactions: []Reaction{{Emoji: "✅", Count: 3}}},
		l("you: I will prepare a short terminal recording as backup"))

	add(Conversation{ID: "C5", Name: "tools-oncall", Topic: "Pager rotation, incidents", Private: true}, today,
		l("erin: @you and dave are on call this week"),
		line{text: "dave: who can look at the runner disk alert?", replies: []string{
			"you: I will take it",
			"erin: thanks; let us know if it escalates",
		}})
	add(Conversation{ID: "C6", Name: "leads", Topic: "Team leads", Private: true}, yesterday,
		l("ana: Monday's agenda is in the usual planning doc"))
	add(general, today.Add(30*time.Minute),
		l("carol: @you, could you review the runner change when you have a moment?"))
	add(Conversation{ID: "C3", Name: "random", Topic: "Non-work banter"}, today,
		line{text: "frank: pizza or sushi for the offsite?", replies: []string{"ana: sushi", "bob: pizza, no debate", "you: sushi, obviously", "frank: we can order both"}},
		line{text: "ana: I added the menu to the thread", reactions: []Reaction{{Emoji: "😂", Count: 4}}})
	add(Conversation{ID: "C7", Name: "help", Topic: "Trying sltty's demo features"}, today,
		line{text: "sltty: Welcome to the demo workspace. Press ? for every keybinding and :help for commands."},
		line{text: "sltty: Toggle inline pictures with I or :images. This message has four pictures; press v to view them as a carousel.", files: []string{"F1", "F2", "F3", "F4"}},
		line{text: "sltty: Try :w to save your UI state, :wq to save and quit, and Tab to switch between the sidebar and chat."})

	var deploys []line
	for i := 1; i <= 40; i++ {
		deploys = append(deploys, l(fmt.Sprintf("bot: deploy #%d finished in %ds — all checks green, nothing to see here, just a long line to test wrapping in narrow panes", 1000+i, 30+i%17)))
	}
	add(Conversation{ID: "C4", Name: "deploys", Topic: "CI/CD notifications", Unread: 12}, today, deploys...)

	add(Conversation{ID: "D1", Name: "ana", Kind: DM, Unread: 1, Online: true}, today,
		l("ana: do you have five minutes to review my merge request?"),
		l("you: sure, send it over"),
		l("ana: it is the HTTP client timeout change"),
		l("ana: the reproduction steps are in the description"))
	add(Conversation{ID: "D2", Name: "bob", Kind: DM}, yesterday,
		line{text: "bob: I owe you a coffee ☕", reactions: []Reaction{{Emoji: "☕", Count: 1}}},
		line{text: "bob: here is the diagram I mentioned :party-parrot:", files: []string{"F2"}})
	add(Conversation{ID: "D3", Name: "carol", Kind: DM, Online: true}, today,
		l("carol: are you shipping the runner change, or should I do it?"))
	add(Conversation{ID: "D4", Name: "you", Kind: DM}, today,
		l("you: scratchpad: compare the sidebar density before the demo"),
		l("you: test links, attachment cards, and unread markers here"),
		l("you: remember to save the preferred layout before quitting"))
	return f
}

func (f *Fake) has(id string) bool {
	for _, c := range f.convs {
		if c.ID == id {
			return true
		}
	}
	return false
}

func (f *Fake) newMsg(s string, at time.Time) Message {
	f.seq++
	user, text, _ := strings.Cut(s, ": ")
	return Message{TS: strconv.Itoa(f.seq), User: user, UserID: user, Text: text, At: at}
}

func (f *Fake) SetPresence(context.Context, bool) error { return nil }
func (f *Fake) SetSnooze(context.Context, int) error    { return nil }

func (f *Fake) Me() string        { return "you" }
func (f *Fake) Workspace() string { return "sltty (demo)" }

func (f *Fake) Conversations(context.Context) ([]Conversation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Conversation(nil), f.convs...), nil
}

func (f *Fake) History(_ context.Context, id string) ([]Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.convs {
		if f.convs[i].ID == id {
			f.convs[i].Unread = 0
			return append([]Message(nil), f.msgs[id]...), nil
		}
	}
	return nil, fmt.Errorf("unknown conversation %q", id)
}

func (f *Fake) Replies(_ context.Context, id, root string) ([]Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, m := range f.msgs[id] {
		if m.TS == root {
			return append([]Message{m}, f.replies[id+"/"+root]...), nil
		}
	}
	return nil, fmt.Errorf("unknown message %q in %q", root, id)
}

func (f *Fake) Send(_ context.Context, id, root, text string) (Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.has(id) {
		return Message{}, fmt.Errorf("unknown conversation %q", id)
	}
	f.seq++
	m := Message{TS: strconv.Itoa(f.seq), User: f.Me(), Text: text, At: f.now()}
	if root == "" {
		f.msgs[id] = append(f.msgs[id], m)
		return m, nil
	}
	msgs := f.msgs[id]
	for i := range msgs {
		if msgs[i].TS == root {
			f.replies[id+"/"+root] = append(f.replies[id+"/"+root], m)
			msgs[i].Replies++
			msgs[i].LastReply = m.At
			return m, nil
		}
	}
	return Message{}, fmt.Errorf("unknown message %q in %q", root, id)
}

// find returns the message ts of a conversation, root or reply, to change in
// place. Callers hold f.mu.
func (f *Fake) find(id, ts string) *Message {
	for i := range f.msgs[id] {
		if f.msgs[id][i].TS == ts {
			return &f.msgs[id][i]
		}
	}
	for k, rs := range f.replies {
		if strings.HasPrefix(k, id+"/") {
			for i := range rs {
				if rs[i].TS == ts {
					return &rs[i]
				}
			}
		}
	}
	return nil
}

func (f *Fake) Edit(_ context.Context, id, ts, text string) (Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m := f.find(id, ts)
	switch {
	case m == nil:
		return Message{}, fmt.Errorf("unknown message %q in %q", ts, id)
	case m.User != f.Me():
		return Message{}, fmt.Errorf("only your own messages can be edited")
	}
	m.Text, m.Edited = text, true
	return *m, nil
}

func (f *Fake) Mark(_ context.Context, id, ts string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.convs {
		if f.convs[i].ID == id {
			// after ts by position: the demo's ts do not sort as text
			n := len(f.msgs[id]) - 1 - slices.IndexFunc(f.msgs[id], func(m Message) bool { return m.TS == ts })
			if ts == "" {
				n = len(f.msgs[id])
			}
			f.convs[i].Unread = n
			return nil
		}
	}
	return fmt.Errorf("unknown conversation %q", id)
}

func (f *Fake) React(_ context.Context, id, ts, emoji string) (Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m := f.find(id, ts)
	if m == nil {
		return Message{}, fmt.Errorf("unknown message %q in %q", ts, id)
	}
	rs := append([]Reaction(nil), m.Reactions...)
	i := slices.IndexFunc(rs, func(r Reaction) bool { return r.Emoji == emoji })
	switch {
	case i < 0:
		rs = append(rs, Reaction{Emoji: emoji, Count: 1, Mine: true})
	case rs[i].Mine:
		rs[i].Count--
		rs[i].Mine = false
		if rs[i].Count == 0 {
			rs = slices.Delete(rs, i, i+1)
		}
	default:
		rs[i].Count++
		rs[i].Mine = true
	}
	m.Reactions = rs
	return *m, nil
}

func (f *Fake) Search(_ context.Context, q string) ([]Hit, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	pq := ParseQuery(q)
	if len(pq.Words) == 0 && pq.In == "" && pq.From == "" {
		return nil, nil
	}
	var hits []Hit
	match := func(m Message) bool {
		if pq.From != "" && strings.ToLower(m.User) != pq.From {
			return false
		}
		for _, w := range pq.Words {
			if !strings.Contains(strings.ToLower(m.Text), strings.ToLower(w)) {
				return false
			}
		}
		return true
	}
	for _, c := range f.convs {
		if pq.In != "" && strings.ToLower(c.Name) != pq.In {
			continue
		}
		for _, m := range f.msgs[c.ID] {
			if match(m) {
				hits = append(hits, Hit{ConvID: c.ID, Msg: m})
			}
			for _, r := range f.replies[c.ID+"/"+m.TS] {
				if match(r) {
					hits = append(hits, Hit{ConvID: c.ID, RootTS: m.TS, Msg: r})
				}
			}
		}
	}
	slices.SortStableFunc(hits, func(a, b Hit) int { return b.Msg.At.Compare(a.Msg.At) })
	return hits[:min(len(hits), 50)], nil
}

// scriptReactions has others react to some of the user's messages.
func (f *Fake) scriptReactions() {
	for _, r := range []struct{ conv, text, by, emoji string }{
		{"D1", "sure, send it over", "ana", "🙏"},
		{"C5", "I will take it", "erin", "💯"},
	} {
		m := f.findText(r.conv, r.text)
		if m == nil {
			continue
		}
		m.Reactions = append(m.Reactions, Reaction{Emoji: r.emoji, Count: 1, Users: []string{r.by}})
		f.reactedBy = append(f.reactedBy, Activity{
			ID: "r" + m.TS + r.emoji, Kind: ActReaction, ConvID: r.conv, RootTS: f.rootOf(r.conv, m.TS),
			Msg: *m, By: r.by, ByID: r.by, Emoji: r.emoji, At: m.At.Add(3 * time.Minute), Unread: true,
		})
	}
}

func (f *Fake) findText(conv, text string) *Message {
	for _, m := range f.allMsgs(conv) {
		if m.Text == text {
			return f.find(conv, m.TS)
		}
	}
	return nil
}

// allMsgs is a conversation's messages and thread replies.
func (f *Fake) allMsgs(conv string) []Message {
	var out []Message
	for _, m := range f.msgs[conv] {
		out = append(out, m)
		out = append(out, f.replies[conv+"/"+m.TS]...)
	}
	return out
}

// rootOf is the thread a reply belongs to, or "" for a top-level message.
func (f *Fake) rootOf(conv, ts string) string {
	for _, m := range f.msgs[conv] {
		for _, r := range f.replies[conv+"/"+m.TS] {
			if r.TS == ts {
				return m.TS
			}
		}
	}
	return ""
}

func (f *Fake) Threads(context.Context) ([]ThreadSummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	me := f.Me()
	var out []ThreadSummary
	for _, c := range f.convs {
		for _, root := range f.msgs[c.ID] {
			rs := f.replies[c.ID+"/"+root.TS]
			mine := root.User == me
			unread := 0
			for _, r := range rs {
				switch {
				case r.User == me:
					mine, unread = true, 0
				case mine:
					unread++
				}
			}
			if !mine || len(rs) == 0 {
				continue
			}
			out = append(out, ThreadSummary{ConvID: c.ID, Root: root, Latest: rs[max(0, len(rs)-2):], Replies: len(rs), Unread: unread})
		}
	}
	slices.SortStableFunc(out, func(a, b ThreadSummary) int {
		return b.Latest[len(b.Latest)-1].At.Compare(a.Latest[len(a.Latest)-1].At)
	})
	return out, nil
}

func (f *Fake) Activity(context.Context) ([]Activity, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	me := f.Me()
	out := append([]Activity(nil), f.reactedBy...)
	for _, c := range f.convs {
		for _, root := range f.msgs[c.ID] {
			if root.User != me && strings.Contains(root.Text, "@"+me) {
				out = append(out, Activity{ID: "m" + root.TS, Kind: ActMention, ConvID: c.ID, Msg: root, By: root.User, ByID: root.UserID, At: root.At, Unread: true})
			}
			in := root.User == me
			for _, r := range f.replies[c.ID+"/"+root.TS] {
				switch {
				case r.User == me:
					in = true
				case in:
					out = append(out, Activity{ID: "t" + r.TS, Kind: ActReply, ConvID: c.ID, RootTS: root.TS, Msg: r, By: r.User, ByID: r.UserID, At: r.At, Unread: true})
				}
			}
		}
	}
	slices.SortStableFunc(out, func(a, b Activity) int { return b.At.Compare(a.At) })
	return out, nil
}
