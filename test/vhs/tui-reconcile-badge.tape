# The pane badge auto-upgrades from the estimated floor ($0.00e) to the reported
# total ($…r) within ~1s of the agent producing output — driven by the live
# usage tick, with NO /cost needed. Setup + fixture live in
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
# The agent produces output → the live tick reconciles the fixture session and
# the badge flips to the reported total ($…r) on its own, without opening /cost.
Wait+Screen@30s /\$[0-9]+\.[0-9]+r/
Screenshot .tmp/vhs/tui-reconcile-badge.png
Sleep 1s

Ctrl+X
Sleep 1s
