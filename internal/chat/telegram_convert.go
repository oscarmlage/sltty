package chat

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/gotd/td/tg"
)

// see remembers which conversation a private or basic group message is in.
func (t *Telegram) see(id int, peer tg.PeerClass) {
	switch peer.(type) {
	case *tg.PeerUser, *tg.PeerChat:
		t.mu.Lock()
		t.seen[id] = convID(peer)
		t.mu.Unlock()
	}
}

// keepQuote remembers what a message says, for the replies to it that
// arrive without it (live, or in a later page).
func (t *Telegram) keepQuote(conv string, id int, q string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.quotes) > 20000 {
		t.quotes = map[string]string{}
	}
	t.quotes[conv+"/"+strconv.Itoa(id)] = q
}

func (t *Telegram) quote(conv string, id int) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.quotes[conv+"/"+strconv.Itoa(id)]
}

// convert turns a Telegram message into a Message. byID holds the other
// messages of the same batch, for reply quotes. ok is false for what is not
// shown (empty messages).
func (t *Telegram) convert(m tg.MessageClass, byID map[int]tg.MessageClass) (Message, bool) {
	switch m := m.(type) {
	case *tg.Message:
		return t.convertMessage(m, byID), true
	case *tg.MessageService:
		return Message{
			TS:     strconv.Itoa(m.ID),
			User:   t.senderName(m.FromID, m.PeerID, m.Out, ""),
			UserID: avatarID(m.FromID, m.PeerID),
			Text:   "_" + t.serviceText(m.Action) + "_",
			At:     time.Unix(int64(m.Date), 0),
		}, true
	}
	return Message{}, false
}

func (t *Telegram) convertMessage(m *tg.Message, byID map[int]tg.MessageClass) Message {
	t.see(m.ID, m.PeerID)
	text := Sanitize(m.Message)
	out := Message{
		TS:     strconv.Itoa(m.ID),
		User:   t.senderName(m.FromID, m.PeerID, m.Out, m.PostAuthor),
		UserID: avatarID(m.FromID, m.PeerID),
		Text:   entitiesToMrkdwn(text, m.Entities),
		At:     time.Unix(int64(m.Date), 0),
		Edited: m.EditDate != 0 && !m.EditHide,
	}
	if out.Text != text {
		out.Raw = text
	}
	if r, ok := m.GetReplies(); ok && r.Replies > 0 {
		out.Replies = r.Replies
	}
	if fwd, ok := m.GetFwdFrom(); ok {
		from := Sanitize(fwd.FromName)
		if p, ok := fwd.GetFromID(); ok {
			from = t.peerName(p)
		}
		if from == "" {
			from = "someone"
		}
		out.Context = append(out.Context, "↪ forwarded from "+from)
	}
	if h, ok := m.ReplyTo.(*tg.MessageReplyHeader); ok {
		// In a forum, every message points at its topic: only a reply
		// within the topic (ReplyToTopID set) is a quote.
		if id := h.ReplyToMsgID; id != 0 && !(h.ForumTopic && h.ReplyToTopID == 0) {
			quote := ""
			if h.QuoteText != "" {
				quote = Sanitize(h.QuoteText)
			}
			cached := t.quote(convID(m.PeerID), id)
			if r, ok := byID[id].(*tg.Message); ok {
				if quote == "" {
					quote = Sanitize(r.Message)
				}
				out.Context = append(out.Context, "↩ "+t.senderName(r.FromID, r.PeerID, r.Out, r.PostAuthor)+": "+snippet(quote))
			} else if cached != "" {
				out.Context = append(out.Context, "↩ "+cached)
			} else if quote != "" {
				out.Context = append(out.Context, "↩ "+snippet(quote))
			} else if !h.ForumTopic {
				out.Context = append(out.Context, "↩ in reply to an earlier message")
			}
		}
	}
	t.media(&out, m.Media)
	t.keepQuote(convID(m.PeerID), m.ID, out.User+": "+snippet(text))
	if rs, ok := m.GetReactions(); ok {
		out.Reactions = t.reactions(rs)
	}
	return out
}

func snippet(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 60 {
		return string(r[:60]) + "…"
	}
	return s
}

// senderName is who wrote a message: the user, or the channel itself for a
// channel post (with its signature when there is one).
func (t *Telegram) senderName(from, peer tg.PeerClass, out bool, author string) string {
	if out {
		return t.Me()
	}
	if from == nil {
		from = peer
	}
	name := t.peerName(from)
	if author != "" {
		name += " (" + Sanitize(author) + ")"
	}
	return name
}

func avatarID(from, peer tg.PeerClass) string {
	if from == nil {
		from = peer
	}
	return convID(from)
}

func reactionGlyph(r tg.ReactionClass) string {
	switch r := r.(type) {
	case *tg.ReactionEmoji:
		return r.Emoticon
	case *tg.ReactionPaid:
		return "⭐"
	case *tg.ReactionCustomEmoji:
		return "✳️"
	}
	return ""
}

func (t *Telegram) reactions(rs tg.MessageReactions) []Reaction {
	var out []Reaction
	for _, rc := range rs.Results {
		g := reactionGlyph(rc.Reaction)
		if g == "" {
			continue
		}
		_, mine := rc.GetChosenOrder()
		r := Reaction{Emoji: g, Count: rc.Count, Mine: mine}
		for _, p := range rs.RecentReactions {
			if reactionGlyph(p.Reaction) == g {
				r.Users = append(r.Users, t.peerName(p.PeerID))
			}
		}
		out = append(out, r)
	}
	return out
}

func (t *Telegram) media(out *Message, media tg.MessageMediaClass) {
	switch md := media.(type) {
	case nil, *tg.MessageMediaEmpty:
	case *tg.MessageMediaPhoto:
		p, ok := md.Photo.(*tg.Photo)
		if !ok {
			out.Context = append(out.Context, "🖼 photo (expired)")
			return
		}
		if f, ok := t.photoFile(p); ok {
			out.Files = append(out.Files, f)
		}
	case *tg.MessageMediaDocument:
		d, ok := md.Document.(*tg.Document)
		if !ok {
			out.Context = append(out.Context, "📎 file (expired)")
			return
		}
		t.document(out, d, md)
	case *tg.MessageMediaWebPage:
		if w, ok := md.Webpage.(*tg.WebPage); ok && (w.Title != "" || w.Description != "") {
			a := Attachment{Color: "#5eb5f7", Pretext: "", Title: Sanitize(w.Title), TitleURL: safeLink(w.URL),
				Text: Sanitize(snippetLines(w.Description, 3)), Footer: Sanitize(w.SiteName)}
			out.Attachments = append(out.Attachments, a)
		}
	case *tg.MessageMediaPoll:
		a := Attachment{Color: "#6ab3f3", Title: "📊 " + Sanitize(md.Poll.Question.Text)}
		var opts []string
		for _, ans := range md.Poll.Answers {
			opts = append(opts, "• "+Sanitize(ans.GetText().Text))
		}
		a.Text = strings.Join(opts, "\n")
		out.Attachments = append(out.Attachments, a)
	case *tg.MessageMediaGeo, *tg.MessageMediaGeoLive:
		out.Context = append(out.Context, "📍 location")
	case *tg.MessageMediaVenue:
		out.Context = append(out.Context, "📍 "+Sanitize(md.Title)+", "+Sanitize(md.Address))
	case *tg.MessageMediaContact:
		out.Context = append(out.Context, "👤 "+Sanitize(strings.TrimSpace(md.FirstName+" "+md.LastName))+" "+Sanitize(md.PhoneNumber))
	case *tg.MessageMediaDice:
		out.Text = strings.TrimSpace(out.Text + " " + md.Emoticon + " " + strconv.Itoa(md.Value))
	default:
		out.Context = append(out.Context, "["+strings.TrimPrefix(fmt.Sprintf("%T", media), "*tg.MessageMedia")+"]")
	}
}

func snippetLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = append(lines[:n], "…")
	}
	return strings.Join(lines, "\n")
}

func photoSize(s tg.PhotoSizeClass) (typ string, w, h int, ok bool) {
	switch s := s.(type) {
	case *tg.PhotoSize:
		return s.Type, s.W, s.H, true
	case *tg.PhotoSizeProgressive:
		return s.Type, s.W, s.H, true
	}
	return "", 0, 0, false
}

func (t *Telegram) photoFile(p *tg.Photo) (File, bool) {
	type size struct {
		typ  string
		w, h int
	}
	var sizes []size
	for _, s := range p.Sizes {
		if typ, w, h, ok := photoSize(s); ok {
			sizes = append(sizes, size{typ, w, h})
		}
	}
	if len(sizes) == 0 {
		return File{}, false
	}
	sort.Slice(sizes, func(i, j int) bool { return sizes[i].w*sizes[i].h < sizes[j].w*sizes[j].h })
	full := sizes[len(sizes)-1]
	thumb := full
	for _, s := range sizes { // the largest up to 800px
		if s.w <= 800 && s.h <= 800 {
			thumb = s
		}
	}
	loc := func(typ string) tg.InputFileLocationClass {
		return &tg.InputPhotoFileLocation{ID: p.ID, AccessHash: p.AccessHash, FileReference: p.FileReference, ThumbSize: typ}
	}
	id := "p" + strconv.FormatInt(p.ID, 10)
	t.mu.Lock()
	t.files[id] = tgFile{thumb: loc(thumb.typ), full: loc(full.typ)}
	t.mu.Unlock()
	return File{ID: id, Name: "photo.jpg", Mime: "image/jpeg", W: full.w, H: full.h}, true
}

func (t *Telegram) document(out *Message, d *tg.Document, md *tg.MessageMediaDocument) {
	name, w, h := "", 0, 0
	var sticker, voice, video, audio, round, gif bool
	alt := ""
	for _, a := range d.Attributes {
		switch a := a.(type) {
		case *tg.DocumentAttributeFilename:
			name = Sanitize(a.FileName)
		case *tg.DocumentAttributeImageSize:
			w, h = a.W, a.H
		case *tg.DocumentAttributeSticker:
			sticker, alt = true, a.Alt
		case *tg.DocumentAttributeAudio:
			audio, voice = true, a.Voice
		case *tg.DocumentAttributeVideo:
			video, round = true, a.RoundMessage
			w, h = a.W, a.H
		case *tg.DocumentAttributeAnimated:
			gif = true
		}
	}
	id := "d" + strconv.FormatInt(d.ID, 10)
	loc := &tg.InputDocumentFileLocation{ID: d.ID, AccessHash: d.AccessHash, FileReference: d.FileReference}
	// The largest thumbnail stands in for what cannot be drawn.
	var thumb tg.InputFileLocationClass
	tw, th := 0, 0
	for _, s := range d.Thumbs {
		if typ, sw, sh, ok := photoSize(s); ok && sw*sh > tw*th {
			tw, th = sw, sh
			thumb = &tg.InputDocumentFileLocation{ID: d.ID, AccessHash: d.AccessHash, FileReference: d.FileReference, ThumbSize: typ}
		}
	}
	image := d.MimeType == "image/jpeg" || d.MimeType == "image/png" || d.MimeType == "image/gif"
	switch {
	case sticker:
		out.Text = strings.TrimSpace(out.Text + " " + alt + " " + "_sticker_")
		return
	case voice:
		out.Context = append(out.Context, "🎤 voice message")
		return
	case round:
		out.Context = append(out.Context, "📹 video message")
		return
	case audio:
		out.Context = append(out.Context, "🎵 "+cmpOr(name, "audio"))
		return
	case image && !gif && !video:
		t.mu.Lock()
		t.files[id] = tgFile{thumb: loc, full: loc}
		t.mu.Unlock()
		out.Files = append(out.Files, File{ID: id, Name: cmpOr(name, "image"), Mime: d.MimeType, W: w, H: h})
		return
	case (video || gif) && thumb != nil:
		t.mu.Lock()
		t.files[id] = tgFile{thumb: thumb, full: thumb}
		t.mu.Unlock()
		label := "🎬 video"
		if gif {
			label = "🎞 GIF"
		}
		out.Context = append(out.Context, label+" "+name)
		out.Files = append(out.Files, File{ID: id, Name: cmpOr(name, "video"), Mime: "image/jpeg", W: max(w, tw), H: max(h, th)})
		return
	}
	out.Files = append(out.Files, File{ID: id, Name: cmpOr(name, "file"), Mime: d.MimeType})
	out.Context = append(out.Context, fmt.Sprintf("📎 %s (%s)", cmpOr(name, "file"), humanSize(d.Size)))
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n>>10)
	}
	return fmt.Sprintf("%d B", n)
}

func (t *Telegram) serviceText(a tg.MessageActionClass) string {
	switch a := a.(type) {
	case *tg.MessageActionChatCreate:
		return "created the group " + Sanitize(a.Title)
	case *tg.MessageActionChannelCreate:
		return "created the channel " + Sanitize(a.Title)
	case *tg.MessageActionChatEditTitle:
		return "changed the title to " + Sanitize(a.Title)
	case *tg.MessageActionChatEditPhoto:
		return "changed the photo"
	case *tg.MessageActionChatAddUser:
		var names []string
		for _, id := range a.Users {
			names = append(names, t.peerName(&tg.PeerUser{UserID: id}))
		}
		return "added " + strings.Join(names, ", ")
	case *tg.MessageActionChatDeleteUser:
		return "removed " + t.peerName(&tg.PeerUser{UserID: a.UserID})
	case *tg.MessageActionChatJoinedByLink, *tg.MessageActionChatJoinedByRequest:
		return "joined the group"
	case *tg.MessageActionPinMessage:
		return "pinned a message"
	case *tg.MessageActionPhoneCall:
		return "📞 call"
	case *tg.MessageActionGroupCall:
		return "🎙 voice chat"
	case *tg.MessageActionTopicCreate:
		return "created the topic " + Sanitize(a.Title)
	case *tg.MessageActionContactSignUp:
		return "joined Telegram"
	case *tg.MessageActionHistoryClear:
		return "cleared the history"
	}
	return strings.ToLower(strings.TrimPrefix(fmt.Sprintf("%T", a), "*tg.MessageAction"))
}

// entitiesToMrkdwn writes Telegram's formatting (entities over UTF-16
// offsets) as the Slack-like markup the TUI renders.
func entitiesToMrkdwn(text string, ents []tg.MessageEntityClass) string {
	u := utf16.Encode([]rune(text))
	opens := map[int][]string{}
	closes := map[int][]string{}
	for _, e := range ents {
		off, n := e.GetOffset(), e.GetLength()
		if off < 0 || n <= 0 || off+n > len(u) {
			continue
		}
		seg := string(utf16.Decode(u[off : off+n]))
		var o, c string
		switch e := e.(type) {
		case *tg.MessageEntityBold:
			o, c = inlineMark(seg, "*")
		case *tg.MessageEntityItalic:
			o, c = inlineMark(seg, "_")
		case *tg.MessageEntityStrike:
			o, c = inlineMark(seg, "~")
		case *tg.MessageEntityCode:
			o, c = inlineMark(seg, "`")
		case *tg.MessageEntityPre:
			if !strings.Contains(seg, "```") {
				o, c = "```\n", "\n```"
			}
		case *tg.MessageEntityTextURL:
			if u := safeLink(e.URL); u != "" && !strings.ContainsAny(seg, "<>|\n") && !strings.ContainsAny(u, "<>| ") {
				o, c = "<"+u+"|", ">"
			}
		}
		if o == "" {
			continue
		}
		opens[off] = append(opens[off], o)
		closes[off+n] = append([]string{c}, closes[off+n]...)
	}
	if len(opens) == 0 {
		return guardAngles(text)
	}
	var b strings.Builder
	last := 0
	for i := 0; i <= len(u); i++ {
		cs, os := closes[i], opens[i]
		if len(cs) == 0 && len(os) == 0 {
			continue
		}
		b.WriteString(guardAngles(string(utf16.Decode(u[last:i]))))
		last = i
		for _, c := range cs {
			b.WriteString(c)
		}
		for _, o := range os {
			b.WriteString(o)
		}
	}
	b.WriteString(guardAngles(string(utf16.Decode(u[last:]))))
	return b.String()
}

// inlineMark is a mark around seg, if the renderer can read it back: one
// line, no mark inside, no space at the edges.
func inlineMark(seg, mark string) (string, string) {
	if seg == "" || strings.TrimSpace(seg) != seg || strings.Contains(seg, "\n") || strings.Contains(seg, mark) {
		return "", ""
	}
	return mark, mark
}

// guardAngles keeps plain text such as "a <b> c" from reading as a Slack
// <token>.
func guardAngles(s string) string {
	if strings.Contains(s, "<") && strings.Contains(s, ">") {
		return strings.ReplaceAll(s, "<", "‹")
	}
	return s
}
