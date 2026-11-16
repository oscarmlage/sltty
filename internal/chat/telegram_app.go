package chat

import "strconv"

// The application sltty identifies itself as to Telegram. Every MTProto
// client needs one (Telegram Desktop, Telegram X... each ships its own);
// it identifies the program, not the user, so users never deal with it.
// Register it once at https://my.telegram.org → API development tools and
// fill these in, or build with
//
//	-ldflags "-X github.com/oscarmlage/sltty/internal/chat.appID=… -X github.com/oscarmlage/sltty/internal/chat.appHash=…"
//
// (the Makefile passes SLTTY_TELEGRAM_API_ID and SLTTY_TELEGRAM_API_HASH that way).
var (
	appID   = ""
	appHash = ""
)

var builtinApp = func() App {
	n, _ := strconv.Atoi(appID)
	return App{ID: n, Hash: appHash}
}()
