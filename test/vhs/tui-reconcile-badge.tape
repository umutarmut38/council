# The pane badge starts with the live estimated floor (~$), then upgrades to the
# reported total (plain $) after an explicit /cost request. Setup + fixture live in
# scenarios/tui-reconcile-badge.sh.
Output .tmp/vhs/council-vhs-tui-reconcile-badge.gif

Require council

Set Shell "bash"
Set FontSize 16
Set Width 1920
Set Height 1080
Set Padding 24
Set Theme "Dracula"
Set TypingSpeed 60ms
Set PlaybackSpeed 1.0

Type "cd $WORK && council --agents planner ask 'Build it.'"
Enter
Wait+Screen@30s /Agents: planner/
# Typing and output only update the cheap local estimate; no provider scan runs
# on the heartbeat.
Wait+Screen@30s /\| ~\$[0-9]/
Type "/cost"
Enter
Wait+Screen@30s /Confidence/
Escape
# The completed explicit scan can now replace the estimate with reported usage.
Wait+Screen@30s /\| \$[0-9]/
Screenshot .tmp/vhs/tui-reconcile-badge.png
Sleep 1s

Ctrl+X
Sleep 1s
