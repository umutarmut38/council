Output .tmp/vhs/council-vhs-cost-view.gif
Set Shell "bash"
Set FontSize 16
Set Width 1920
Set Height 1080
Set Padding 24
Set Theme "Dracula"
Set TypingSpeed 60ms
Set PlaybackSpeed 1.0

Type "cd $WORK && council --agents priced,unknown ask 'show the cost view'"
Enter
Sleep 4s
Type "/cost"
Enter
Wait+Screen@30s /stale price/
Wait+Screen@30s /usage\.model is not configured/
Wait+Screen@30s /Confidence/
Screenshot .tmp/vhs/cost-view-cost.png
Sleep 1s
Ctrl+X
Sleep 1s
