# The full council TUI: two priced agents in a grid, cost in the header rail and
# per-pane borders, then the /cost breakdown. Driven by scenarios/tui-overview.sh
# (run via scripts/test-vhs.sh, which sources the sandbox env so $WORK resolves).
Output .tmp/vhs/council-vhs-tui-overview.gif

Require council

Set Shell "bash"
Set FontSize 16
Set Width 1920
Set Height 1080
Set Padding 24
Set Theme "Dracula"
Set TypingSpeed 60ms
Set PlaybackSpeed 1.0

Type "cd $WORK && council --agents alpha,beta ask 'write a short haiku about the sea'"
Enter
# Block until the TUI has painted: the header rail always shows "Agents: …".
Wait+Screen@30s /Agents: alpha, beta/
# Cost is on by default, so the header rail carries the run total.
Wait+Screen@30s /Run \$/
Sleep 3s
Screenshot .tmp/vhs/tui-overview-main.png

# Open the per-session cost breakdown over the live run.
Type "/cost"
Enter
Wait+Screen@30s /Confidence/
Screenshot .tmp/vhs/tui-overview-cost.png
Sleep 1s

Ctrl+X
Sleep 1s
