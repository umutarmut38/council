# Opt-in freestyle worktrees (worktrees.freestyle): two same-tool claude panes
# each launch in their own persistent .council/workspaces/<agent> worktree →
# per-pane reported cost + a stale (*) marker → /refresh resets it. Setup +
# asserts in scenarios/freestyle-worktrees.sh.
Output .tmp/vhs/council-vhs-freestyle-worktrees.gif

Require council

Set Shell "bash"
Set FontSize 16
Set Width 1920
Set Height 1080
Set Padding 24
Set Theme "Dracula"
Set TypingSpeed 60ms
Set PlaybackSpeed 1.0

Type "cd $WORK && council --agents claude-a,claude-b"
Enter
Wait+Screen@30s /Agents: claude-a, claude-b/
Wait+Screen@30s /claude ready/
Screenshot .tmp/vhs/freestyle-worktrees-boot.png
Type "Build it."
Enter
Wait+Screen@30s /sent to all agents/
# Distinct worktree cwds → each pane's badge upgrades to its own reported cost
# (plain "| $…", not the estimated "| ~$…").
Wait+Screen@30s /\| \$[0-9]/
# The fakes dirtied their worktrees; wait for the slow status tick to mark them.
Sleep 7s
Screenshot .tmp/vhs/freestyle-worktrees-stale.png
Type "/cost"
Enter
Wait+Screen@30s /Confidence/
Screenshot .tmp/vhs/freestyle-worktrees-cost.png
Escape
Sleep 1s
Type "/refresh all force"
Enter
Wait+Screen@30s /refreshed/
Screenshot .tmp/vhs/freestyle-worktrees-refreshed.png
Sleep 1s

Ctrl+X
Sleep 1s
