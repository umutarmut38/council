Output .tmp/vhs/council-vhs-codeburn-missing.gif
Set Shell "bash"
Set FontSize 16
Set Width 1920
Set Height 1080
Set Padding 24
Set Theme "Dracula"
Set TypingSpeed 60ms
Set PlaybackSpeed 1.0

Type "cd $WORK && PATH=$ROOT/bin:$CASE_DIR/bin:/usr/bin:/bin council cost --source codeburn"
Enter
Wait+Screen@30s /codeburn is not installed/
Wait+Screen@30s /machine-wide totals/
Screenshot .tmp/vhs/codeburn-missing.png
Sleep 1s
