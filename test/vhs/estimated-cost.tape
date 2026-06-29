Output .tmp/vhs/council-vhs-estimated-cost.gif
Set Shell "bash"
Set FontSize 16
Set Width 1920
Set Height 1080
Set Padding 24
Set Theme "Dracula"
Set TypingSpeed 60ms
Set PlaybackSpeed 1.0

Type "cd $WORK && council --agents fake ask 'estimate a priced prompt'"
Enter
Sleep 4s
Type "/cost"
Enter
Wait+Screen@30s /gpt-5/
Wait+Screen@30s /litellm-bundled/
Screenshot .tmp/vhs/estimated-cost-cost.png
Sleep 1s
Ctrl+X
Sleep 1s
