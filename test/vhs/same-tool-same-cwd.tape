Output .tmp/vhs/council-vhs-same-tool-same-cwd.gif
Set Shell "bash"
Set FontSize 16
Set Width 1920
Set Height 1080
Set Padding 24
Set Theme "Dracula"
Set TypingSpeed 60ms
Set PlaybackSpeed 1.0

Type "cd $WORK && council cost 20260628-same-tool"
Enter
Wait+Screen@30s /claude-a/
Wait+Screen@30s /claude-b/
Wait+Screen@30s /litellm-bundled/
Screenshot .tmp/vhs/same-tool-same-cwd-cost.png
Sleep 1s
