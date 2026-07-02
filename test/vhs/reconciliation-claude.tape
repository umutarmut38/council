Output .tmp/vhs/council-vhs-reconciliation-claude.gif
Set Shell "bash"
Set FontSize 16
Set Width 1920
Set Height 1080
Set Padding 24
Set Theme "Dracula"
Set TypingSpeed 60ms
Set PlaybackSpeed 1.0

Type "cd $WORK && council cost 20260628-reconcile"
Enter
Wait+Screen@30s /claude-sonnet-4-6/
Wait+Screen@30s /litellm-bundled/
Screenshot .tmp/vhs/reconciliation-claude-cost.png
Sleep 1s
