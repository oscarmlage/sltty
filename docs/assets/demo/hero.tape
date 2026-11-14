# README hero: messages, threads, reactions, finder, Activity, Settings and Help.
Output docs/assets/demo/hero.gif

Hide
Type "./bin/sltty -demo"
Enter
Sleep 1.5s
Show

# Browse messages, then switch with the fuzzy finder.
Type "l"
Sleep 500ms
Type "jj"
Sleep 700ms
Ctrl+K
Sleep 400ms
Type "#tools-team"
Sleep 500ms
Enter
Sleep 1s

# A reaction's people, in horizontal emoji tabs.
Type "k"
Sleep 400ms
Type "R"
Sleep 1.5s
Type "l"
Sleep 900ms
Escape

# Open a thread and write a reply.
Type "gg"
Sleep 500ms
Type "t"
Sleep 900ms
Type "i"
Type "Looks good from the terminal."
Sleep 600ms
Enter
Sleep 1s
Escape

# Activity uses the native Slack-style notification feed.
Ctrl+K
Sleep 400ms
Type "activity"
Sleep 500ms
Enter
Sleep 1.5s

# Settings and the compact tabbed keybinding help.
Type ";"
Sleep 1.2s
Type "jll"
Sleep 900ms
Escape
Type "?"
Sleep 1.2s
Type "l"
Sleep 800ms
Type "l"
Sleep 800ms
Type "l"
Sleep 800ms

# Status picker: Ctrl+S opens it, then select Away.
Escape
Ctrl+S
Sleep 700ms
Type "away"
Sleep 400ms
Enter
Sleep 900ms

# The command line opens the same picker; choose a snooze duration.
Type ":status"
Enter
Sleep 700ms
Type "snooze for 20"
Sleep 400ms
Enter
Sleep 900ms
