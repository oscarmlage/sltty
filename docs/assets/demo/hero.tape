# README hero: the whole tour in one recording.
Output docs/assets/demo/hero.gif

Hide
Type "./bin/sltty -demo"
Enter
Sleep 1.5s
Show

# Messages, with pictures and avatars.
Sleep 1s
Type "l"
Sleep 500ms
Type "kk"
Sleep 900ms

# Fuzzy finder: # for channels. Open the one with the pictures; v views them.
Ctrl+K
Sleep 400ms
Type "#help"
Sleep 700ms
Enter
Sleep 1.2s
Type "k"
Sleep 500ms
Type "v"
Sleep 1.4s
Type "l"
Sleep 900ms
Escape
Sleep 600ms

# Threads: open one and reply.
Ctrl+K
Sleep 400ms
Type "random"
Sleep 600ms
Enter
Sleep 800ms
Type "gg"
Sleep 400ms
Enter
Sleep 1.2s
Type "i"
Type "Both, plese."
Sleep 500ms
Enter
Sleep 1.2s

# A typo: up arrow in the empty composer edits the last message.
Up
Sleep 900ms
Backspace
Backspace
Backspace
Backspace
Backspace
Backspace
Sleep 300ms
Type "please."
Sleep 600ms
Enter
Sleep 1.4s
Escape
Sleep 500ms
Escape
Sleep 700ms

# Who reacted, in tabs per emoji.
Ctrl+K
Sleep 400ms
Type "tools-team"
Sleep 500ms
Enter
Sleep 800ms
Type "k"
Sleep 400ms
Type "R"
Sleep 1.3s
Type "l"
Sleep 900ms
Escape
Sleep 500ms

# Links, from the keyboard: o lists them, a key opens one.
Type "kkk"
Sleep 600ms
Type "o"
Sleep 2.2s
Escape
Sleep 600ms

# Search with Slack's syntax.
Ctrl+F
Sleep 400ms
Type "runner"
Sleep 1.4s
Enter
Sleep 1.2s
# n / N walk through the matches.
Type "n"
Sleep 1s
Type "n"
Sleep 1s
Type "N"
Sleep 1s
Escape
Sleep 500ms

# Back to #tools-team for the themes.
Ctrl+K
Sleep 400ms
Type "tools-team"
Sleep 500ms
Enter
Sleep 1s

# Themes preview live while you move through them.
Type "T"
Sleep 800ms
Down
Sleep 800ms
Down
Sleep 800ms
Down
Sleep 800ms
Down
Sleep 800ms
Down
Sleep 800ms
Up
Sleep 700ms
Up
Sleep 700ms
Enter
Sleep 900ms

# Another layout.
Type "L"
Sleep 1.3s
Type "L"
Sleep 800ms

# Activity feed, then the command line.
Ctrl+K
Sleep 400ms
Type "activity"
Sleep 500ms
Enter
Sleep 1.4s
Type ":theme aubergine"
Sleep 500ms
Enter
Sleep 900ms

# Settings: toggling pictures shows its effect live.
Ctrl+K
Sleep 400ms
Type "help"
Sleep 500ms
Enter
Sleep 1.8s
Type ";"
Sleep 1.2s
Type "jjj"
Sleep 1s
Type "l"
Sleep 2.2s
Type "h"
Sleep 2.4s
Escape
Sleep 600ms

# The key list.
Type "?"
Sleep 1.2s
Type "l"
Sleep 700ms
Type "l"
Sleep 900ms
Escape
Sleep 600ms
