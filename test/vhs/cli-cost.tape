Output .tmp/vhs/council-vhs-cli-cost.gif
Set Shell "bash"
Set FontSize 16
Set Width 1920
Set Height 1080
Set Padding 24
Set Theme "Dracula"
Set TypingSpeed 60ms
Set PlaybackSpeed 1.0

Type "cd $WORK && council cost 20260628-cli-cost"
Enter
Wait+Screen@30s /local-free/
Wait+Screen@30s /\$0\.00/
Screenshot .tmp/vhs/cli-cost.png
Sleep 1s
